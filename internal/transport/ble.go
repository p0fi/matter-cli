// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package transport

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// bleWriteRetryInitialBackoff is the initial delay before retrying a
	// C1 Write Without Response that was deferred because
	// canSendWriteWithoutResponse was false.
	bleWriteRetryInitialBackoff = 5 * time.Millisecond

	// bleWriteRetryMaxBackoff caps the exponential backoff between retries.
	bleWriteRetryMaxBackoff = 200 * time.Millisecond

	// bleWriteRetryMaxAttempts is the number of Write Without Response
	// attempts before falling back to Write With Response.
	bleWriteRetryMaxAttempts = 50

	// bleAckTickInterval is how often we check whether a standalone BTP
	// ACK needs to be sent.  This must be shorter than btpAckTimeout so
	// the ACK is dispatched before the peer times out.
	bleAckTickInterval = 500 * time.Millisecond
)

// BLEConn implements transport.Conn over a BLE GATT connection using the BTP
// protocol (Matter Specification §4.15). It provides the same datagram
// abstraction as UDPConn, allowing the entire Matter protocol stack to operate
// identically regardless of the underlying transport.
//
// BLEConn is safe for concurrent use by multiple goroutines.
type BLEConn struct {
	device    bleDevice
	c1        bleCharacteristic // Write (commissioner → device)
	c2        bleCharacteristic // Read (device → commissioner, indication-based)
	btp       *btpSession
	peerAddr  net.Addr
	closed    chan struct{}
	closeOnce sync.Once

	// c1Mu serialises all writes to the C1 characteristic.  Without this,
	// the standalone-ACK timer goroutine can inject a write between two
	// segments of a multi-segment BTP send, causing the device to see
	// out-of-order sequence numbers (the ACK's seqNum was allocated after
	// all data-segment seqNums but is written in the middle of them).
	c1Mu sync.Mutex
}

// BLEAddr implements net.Addr for a BLE peer device.
type BLEAddr struct {
	Address BLEAddress
}

// Network returns the network name "ble".
func (a *BLEAddr) Network() string { return "ble" }

// String returns the opaque BLE address string.
func (a *BLEAddr) String() string { return a.Address.String() }

// DialBLE connects to a Matter device at the given BLE address, discovers the
// Matter GATT service, and completes the BTP handshake. It returns a ready-to-
// use BLEConn that satisfies transport.Conn.
//
// The adapter must be enabled (adapter.Enable()) before calling DialBLE.
func DialBLE(ctx context.Context, adapter bleAdapter, addr BLEAddress) (*BLEConn, error) {
	// 1. Connect to the device.
	slog.Debug("ble: connecting to device", "addr", addr)
	device, err := adapter.Connect(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("ble: connecting to %s: %w", addr, err)
	}
	slog.Debug("ble: GATT connected")

	// 2. Discover the Matter service.
	slog.Debug("ble: discovering Matter service")
	services, err := device.DiscoverServices([]BLEUUID{MatterServiceUUID})
	if err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: discovering Matter service: %w", err)
	}
	if len(services) == 0 {
		device.Disconnect()
		return nil, fmt.Errorf("ble: Matter service (UUID %s) not found on device", MatterServiceUUID)
	}
	svc := services[0]

	// 3. Discover C1 and C2 characteristics.
	slog.Debug("ble: discovering characteristics")
	chars, err := svc.DiscoverCharacteristics([]BLEUUID{MatterC1UUID, MatterC2UUID})
	if err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: discovering characteristics: %w", err)
	}
	var c1, c2 bleCharacteristic
	for _, ch := range chars {
		switch ch.UUID() {
		case MatterC1UUID:
			c1 = ch
		case MatterC2UUID:
			c2 = ch
		}
	}
	if c1 == nil || c2 == nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: missing required characteristic (C1=%v, C2=%v)", c1 != nil, c2 != nil)
	}
	slog.Debug("ble: characteristic discovery complete", "c1", c1.UUID(), "c2", c2.UUID())

	// 4. Set up BTP session.
	btp := newBTPSession()

	// 5. BTP handshake.
	//
	// The CHIP SDK (BLEEndPoint::StartConnect → HandleHandshakeConfirmationReceived)
	// performs the BTP handshake in this exact order:
	//
	//   1. Write BTP Capabilities Request to C1 (GATT Write Request)
	//   2. Wait for write confirmation
	//   3. Subscribe to C2 indications (CCCD write)
	//   4. Peripheral receives subscribe → sends stashed Capabilities Response
	//   5. Central receives C2 indication with response
	//
	// The order matters because the peripheral's state machine (CHIP SDK
	// BLEEndPoint) creates the BTP endpoint only when it receives the C1
	// write (HandleWriteReceived → HandleBleTransportConnectionInitiated).
	// At that point it stashes the Capabilities Response and waits for the
	// C2 subscribe to arrive. If subscribe comes BEFORE the write, the
	// peripheral has no endpoint yet, silently drops the subscribe, and
	// then waits forever for a subscribe that already came and went —
	// eventually timing out and disconnecting.
	//
	// Data delivery: the tinygo notification callback (EnableNotifications)
	// is the sole delivery path for incoming BTP segments. During the
	// handshake phase, the callback forwards BTP capabilities messages to
	// hsResp. After the handshake completes (dataMode flag set), the
	// callback feeds segments directly to btp.handleSegment.
	//
	// For the handshake response only, a parallel cached-value poll
	// (WaitForValue) acts as a backup in case the notification callback
	// fires before we register it. Once the handshake is done, all data
	// flows through the notification callback exclusively.

	// Diagnostic: confirm bt_queue is initialised before we attempt any
	// bt_queue-dispatched operations.
	slog.Debug("ble: bt_queue state", "initialized", corebtIsBTQueueInitialized())

	// ── Step 5a: Write BTP Capabilities Request to C1 ──────────────────────
	//
	// The Matter specification (§4.18) and the CHIP SDK (BLEEndPoint::SendWrite)
	// both require the BTP Capabilities Request to be sent as a GATT Write
	// Request (ATT_WRITE_REQ, with ATT-level response), NOT as a Write Without
	// Response (ATT_WRITE_CMD). Devices will reject or disconnect if they
	// receive the wrong write type on C1 for the handshake.
	hsReq := btpHandshakeRequest(btpSupportedVersionsList, btpDefaultATTMTU, btpDefaultWindowSize)
	slog.Debug("ble: BTP handshake request", "len", len(hsReq), "hex", hex.EncodeToString(hsReq))

	n, err := c1.WriteWithResponse(hsReq)
	if err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: writing BTP capabilities request to C1: %w", err)
	}
	slog.Debug("ble: BTP capabilities request written to C1", "bytesWritten", n)

	// Brief settle: give the peripheral a moment to process the write and
	// create its BTP endpoint before we subscribe.
	time.Sleep(50 * time.Millisecond)

	// ── Step 5b: Subscribe to C2 indications ───────────────────────────────
	//
	// Now that the peripheral has received the capabilities request and
	// created its BTP endpoint, subscribing to C2 will trigger it to send
	// the stashed Capabilities Response via a GATT indication.
	hsResp := make(chan []byte, 1)

	// dataMode is flipped to true after the handshake completes. When true,
	// the notification callback feeds BTP data segments directly to
	// btp.handleSegment instead of ignoring them.
	var dataMode atomic.Bool

	// Register the notification callback — the sole delivery path for all
	// incoming C2 data. During the handshake it forwards BTP capabilities
	// messages to hsResp. After the handshake (dataMode == true), it feeds
	// BTP data segments to btp.handleSegment.
	if err := c2.EnableNotifications(func(data []byte) {
		slog.Debug("ble: C2 data received", "len", len(data), "hex", hex.EncodeToString(data))
		if isBTPCapabilitiesMessage(data) {
			select {
			case hsResp <- data:
			default:
			}
			return
		}
		if dataMode.Load() {
			if err := btp.handleSegment(data); err != nil {
				slog.Debug("ble: BTP segment error", "err", err)
			}
		}
	}); err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: subscribing to C2: %w", err)
	}
	slog.Debug("ble: registered C2 notification callback via EnableNotifications")

	// Wait for the CCCD subscription to be confirmed.
	notifyCtx, notifyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer notifyCancel()
	if err := c2.WaitForNotifying(notifyCtx); err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: waiting for C2 subscription: %w", err)
	}
	slog.Debug("ble: C2 subscription confirmed, waiting for handshake response")

	// ── Step 5c: Wait for the Capabilities Response on C2 ──────────────────
	//
	// The peripheral should send the response indication shortly after the
	// subscribe is confirmed. We use both delivery paths (notification
	// callback and cached-value polling) and accept whichever fires first.
	const (
		btpHandshakeMaxAttempts    = 5
		btpHandshakeRetryInterval  = 3 * time.Second
	)

	// Total handshake budget: 15 s from the start of this step.
	hsTimeout, hsCancel := context.WithTimeout(ctx, 15*time.Second)
	defer hsCancel()

	// Start the Path B poll goroutine.
	go func() {
		slog.Debug("ble: C2 poll goroutine started")
		data, err := c2.WaitForValue(hsTimeout)
		if err != nil {
			slog.Debug("ble: C2 poll goroutine ended", "err", err)
			return
		}
		slog.Debug("ble: C2 data received (poll path)", "len", len(data), "hex", hex.EncodeToString(data))
		if isBTPCapabilitiesMessage(data) {
			select {
			case hsResp <- data:
			default:
			}
		}
	}()

	// sendCapsRequest re-sends the BTP Capabilities Request to C1 using a
	// GATT Write Request (with response). Used for retries if the device
	// does not respond promptly.
	sendCapsRequest := func() error {
		c2.ClearCachedValue()
		nn, err := c1.WriteWithResponse(hsReq)
		if err != nil {
			return fmt.Errorf("ble: C1 WriteWithResponse: %w", err)
		}
		slog.Debug("ble: BTP handshake request re-sent (WriteWithResponse)", "bytesWritten", nn)
		return nil
	}

	// Wait for the response, retrying the write if needed.
	retryTicker := time.NewTicker(btpHandshakeRetryInterval)
	defer retryTicker.Stop()

	attempt := 1
	var hsData []byte
	handshakeDone := false
	for !handshakeDone {
		select {
		case hsData = <-hsResp:
			hsCancel()
			slog.Debug("ble: BTP handshake response received", "attempt", attempt)
			handshakeDone = true

		case <-retryTicker.C:
			attempt++
			if attempt > btpHandshakeMaxAttempts {
				device.Disconnect()
				if ctx.Err() != nil {
					return nil, fmt.Errorf("ble: BTP handshake cancelled: %w", ctx.Err())
				}
				return nil, fmt.Errorf("ble: BTP handshake timed out (tried %d attempts)", attempt-1)
			}
			slog.Debug("ble: BTP handshake no response, retrying", "attempt", attempt)
			if err := sendCapsRequest(); err != nil {
				device.Disconnect()
				return nil, err
			}

		case <-hsTimeout.Done():
			device.Disconnect()
			if ctx.Err() != nil {
				return nil, fmt.Errorf("ble: BTP handshake cancelled: %w", ctx.Err())
			}
			return nil, fmt.Errorf("ble: BTP handshake timed out (tried %d attempts)", attempt)
		}
	}

	version, fragmentSize, windowSize, err := parseBTPHandshakeResponse(hsData)
	if err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: %w", err)
	}

	// 6. Apply negotiated parameters.
	slog.Debug("ble: BTP handshake complete", "version", version, "fragmentSize", fragmentSize, "windowSize", windowSize)
	btp.initHandshakeFromResponse(version, fragmentSize, windowSize)

	conn := &BLEConn{
		device:   device,
		c1:       c1,
		c2:       c2,
		btp:      btp,
		peerAddr: &BLEAddr{Address: addr},
		closed:   make(chan struct{}),
	}

	// 7. Activate data-mode on the notification callback.
	//
	// From this point the callback will feed BTP data segments directly to
	// btp.handleSegment. No separate polling goroutine is needed — the
	// notification callback is the sole delivery path.
	dataMode.Store(true)

	// 8. Start the disconnect watcher goroutine.
	//
	// Periodically checks whether the peripheral is still connected.
	// Without this, a silent disconnection (e.g. device crash) would leave
	// Receive() blocked forever because no more C2 indications will arrive.
	conn.startDisconnectWatcher()

	// 9. Start the standalone ACK timer goroutine.
	//
	// BTP requires the receiver to acknowledge incoming segments within
	// btpAckTimeout.  When outgoing data segments are being sent, the ACK
	// is piggybacked automatically.  But when we only *receive* data (e.g.
	// a large response spanning multiple BTP segments) without sending
	// anything back, we must emit a standalone ACK before the peer times
	// out.  This goroutine periodically checks for a pending ACK and
	// writes it to C1.
	conn.startAckTimer()

	return conn, nil
}

// startDisconnectWatcher launches a background goroutine that periodically
// checks whether the BLE peripheral is still connected. Without this, a
// peripheral that silently disconnects (e.g. crashes during commissioning)
// would leave Receive() blocked forever because no more C2 notifications
// will ever arrive.
//
// It runs until the connection is closed.
func (c *BLEConn) startDisconnectWatcher() {
	if c.c2 == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-c.closed:
				return
			case <-ticker.C:
				if !c.c2.IsConnected() {
					slog.Debug("ble: disconnect watcher detected peripheral disconnection, closing connection")
					c.Close()
					return
				}
			}
		}
	}()
}

// Send implements transport.Conn. The addr parameter is ignored because BLE
// is a point-to-point connection.
func (c *BLEConn) Send(ctx context.Context, msg []byte, _ net.Addr) error {
	select {
	case <-c.closed:
		return fmt.Errorf("ble: %w", ErrConnClosed)
	default:
	}

	// Hold c1Mu for the entire multi-segment write so the standalone-ACK
	// timer cannot inject an out-of-order BTP segment between our data
	// segments.
	c.c1Mu.Lock()
	defer c.c1Mu.Unlock()

	segments := c.btp.segment(msg)
	for i, seg := range segments {
		if err := c.btp.waitCanSend(ctx); err != nil {
			return fmt.Errorf("ble: flow control: %w", err)
		}
		if err := c.writeSegmentC1(ctx, seg, i, len(segments)); err != nil {
			return err
		}
		c.btp.markSent()
	}
	return nil
}

// writeSegmentC1 writes a single BTP segment to C1, retrying with
// exponential backoff when the peripheral reports that
// canSendWriteWithoutResponse is false (Write returns -2, nil).
//
// After bleWriteRetryMaxAttempts failed attempts it falls back to
// WriteWithResponse, which blocks in CoreBluetooth until the ATT-level
// response arrives but is guaranteed to deliver the data.
func (c *BLEConn) writeSegmentC1(ctx context.Context, seg []byte, segIdx, segTotal int) error {
	backoff := bleWriteRetryInitialBackoff

	for attempt := 1; attempt <= bleWriteRetryMaxAttempts; attempt++ {
		select {
		case <-c.closed:
			return fmt.Errorf("ble: %w", ErrConnClosed)
		case <-ctx.Done():
			return fmt.Errorf("ble: writing segment to C1: %w", ctx.Err())
		default:
		}

		n, err := c.c1.Write(seg)
		if err != nil {
			return fmt.Errorf("ble: writing segment to C1: %w", err)
		}
		if n >= 0 {
			// Success.
			return nil
		}

		// n == -2: canSendWriteWithoutResponse is false. Wait and retry.
		if attempt%10 == 0 {
			slog.Debug("ble: C1 write still deferred, retrying",
				"attempt", attempt,
				"segment", fmt.Sprintf("%d/%d", segIdx+1, segTotal),
				"backoff", backoff.String())
		}

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return fmt.Errorf("ble: writing segment to C1: %w", ctx.Err())
		case <-c.closed:
			return fmt.Errorf("ble: %w", ErrConnClosed)
		}

		// Exponential backoff, capped.
		backoff = backoff * 2
		if backoff > bleWriteRetryMaxBackoff {
			backoff = bleWriteRetryMaxBackoff
		}
	}

	// Exhausted Write Without Response retries — fall back to Write With
	// Response. This is slower (waits for ATT-level ACK) but reliable.
	slog.Debug("ble: C1 WriteWithoutResponse exhausted retries, falling back to WriteWithResponse",
		"segment", fmt.Sprintf("%d/%d", segIdx+1, segTotal))
	if _, err := c.c1.WriteWithResponse(seg); err != nil {
		return fmt.Errorf("ble: writing segment to C1 (WriteWithResponse fallback): %w", err)
	}
	return nil
}

// Receive implements transport.Conn. It blocks until a fully reassembled
// Matter message is available or the context is cancelled.
func (c *BLEConn) Receive(ctx context.Context) ([]byte, net.Addr, error) {
	select {
	case msg := <-c.btp.Messages():
		return msg, c.peerAddr, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-c.closed:
		return nil, nil, fmt.Errorf("ble: %w", ErrConnClosed)
	}
}

// startAckTimer launches a background goroutine that periodically checks
// whether the BTP layer has a pending acknowledgement that has not been
// piggybacked on an outgoing data segment. When one is found, a standalone
// ACK is written to C1.
//
// Without this, the peer would time out (btpAckTimeout) during periods when
// we receive data but send nothing — for example while receiving a large
// multi-segment response.
func (c *BLEConn) startAckTimer() {
	go func() {
		ticker := time.NewTicker(bleAckTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-c.closed:
				return
			case <-ticker.C:
				// Acquire c1Mu so we never interleave a standalone ACK
				// in the middle of a multi-segment Send.
				c.c1Mu.Lock()
				ack := c.btp.standaloneAck()
				if ack == nil {
					c.c1Mu.Unlock()
					continue
				}
				slog.Debug("ble: sending standalone BTP ack", "len", len(ack), "hex", hex.EncodeToString(ack))
				n, err := c.c1.Write(ack)
				if err != nil {
					// Hard write error — rollback so the ACK is retried
					// on the next tick.
					c.btp.rollbackStandaloneAck(ack[1])
					slog.Debug("ble: failed to send standalone ack, rolled back", "err", err)
				} else if n == -2 {
					// canSendWriteWithoutResponse is false — the ACK was
					// not delivered.  Rollback the seqNum increment and
					// re-arm hasPendingAck so the next timer tick (or the
					// next outgoing data segment) can retry.  Without
					// rollback the consumed-but-never-sent seqNum creates
					// a gap that makes the peer reject all subsequent
					// segments.
					c.btp.rollbackStandaloneAck(ack[1])
					slog.Debug("ble: standalone ack deferred (canSend=false), rolled back for retry")
				}
				c.c1Mu.Unlock()
			}
		}
	}()
}

// Close implements transport.Conn.
func (c *BLEConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		c.btp.closeSession()
		err = c.device.Disconnect()
	})
	return err
}

// Compile-time check that BLEConn satisfies transport.Conn.
var _ Conn = (*BLEConn)(nil)
