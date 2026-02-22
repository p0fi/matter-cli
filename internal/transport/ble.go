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
	"time"
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

	// 5. BTP handshake: subscribe to C2 first, then write the capabilities
	//    request to C1.
	//
	// The Matter specification (§4.15) defines the BTP handshake order as:
	//   1. Subscribe to C2 indications (CCCD write)
	//   2. Write BTP Capabilities Request to C1
	//   3. Wait for the Capabilities Response on C2
	//
	// The subscription MUST be established before writing the request
	// because the device sends its response indication immediately upon
	// receiving the request. If C2 is not yet subscribed, the BLE stack
	// silently drops the indication and the handshake times out.
	//
	// Data delivery uses two parallel paths that race to deliver values:
	//
	//   Path A – tinygo notification callback (EnableNotifications).
	//   Path B – CoreBluetooth cached-value polling (WaitForValue).
	//
	// Path A can silently fail on macOS because tinygo's
	// DidUpdateValueForCharacteristic delegate uses a pointer-based
	// characteristic match. If the CBCharacteristic pointer captured at
	// discovery time has become stale (no longer pointer-identical to the
	// live CoreBluetooth object), the callback is never invoked even though
	// CoreBluetooth received and stored the indication data.
	//
	// Path B (polling CBCharacteristic.value) is therefore the primary
	// delivery mechanism for the BTP handshake response. Crucially, ALL
	// reads and clears of CBCharacteristic.value are dispatched to bt_queue
	// (the CoreBluetooth serial queue) so that we access the property from
	// the same thread that CoreBluetooth writes it on. Reading from an
	// arbitrary goroutine thread would silently miss updates.
	//
	// After the handshake, a dedicated C2 data-polling goroutine
	// (startC2DataPoller) uses the atomic ReadAndClearCachedValue operation
	// to deliver ongoing BTP segments, completely bypassing tinygo's
	// unreliable notification callback dispatch for data delivery.
	hsResp := make(chan []byte, 1)

	// ── Step 5a: Register the tinygo notification callback (Path A) ──
	//
	// This callback fires only when tinygo's pointer-based dispatch works.
	// For the BTP handshake response it acts as a fast-path backup; for
	// ongoing BTP data segments delivery is handled by the C2 data poller
	// (startC2DataPoller) started after the handshake completes, so we do
	// NOT call btp.handleSegment here to avoid double-delivery.
	if err := c2.EnableNotifications(func(data []byte) {
		slog.Debug("ble: C2 data received (notification callback)", "len", len(data), "hex", hex.EncodeToString(data))
		if isBTPCapabilitiesMessage(data) {
			select {
			case hsResp <- data:
			default:
			}
		}
		// Non-handshake segments are delivered by startC2DataPoller via
		// ReadAndClearCachedValue; handling them here too would double-
		// deliver since CoreBluetooth also updates characteristic.value.
	}); err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: subscribing to C2: %w", err)
	}
	slog.Debug("ble: registered C2 notification callback via EnableNotifications")

	// ── Step 5b: Wait for the CCCD subscription to be confirmed ──
	//
	// On macOS, polls CBCharacteristic.isNotifying. If tinygo's async CCCD
	// write succeeds (normal case with a developer Bluetooth profile), this
	// returns as soon as isNotifying flips to true. If it fails silently, the
	// repair path inside WaitForNotifying issues a fresh setNotifyValue:YES
	// directly on bt_queue.
	notifyCtx, notifyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer notifyCancel()
	if err := c2.WaitForNotifying(notifyCtx); err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: waiting for C2 subscription: %w", err)
	}
	slog.Debug("ble: C2 subscription confirmed, proceeding with handshake")

	// Brief settle: give the peripheral a moment to fully process the CCCD
	// write before we send the capabilities request.
	time.Sleep(50 * time.Millisecond)

	// ── Step 5c: Clear any stale C2 value BEFORE writing to C1 ──
	//
	// This MUST happen before the write, not after. The device sends its BTP
	// Capabilities Response immediately upon receiving the request, so by the
	// time the WaitForValue goroutine below starts, the indication may already
	// have arrived and been stored in characteristic.value. Clearing after the
	// write would erase that already-delivered response.
	//
	// ClearCachedValue dispatches to bt_queue, so it is guaranteed to run
	// before any indication triggered by our subsequent write can land.
	c2.ClearCachedValue()
	slog.Debug("ble: C2 value cleared before handshake write")

	// ── Step 5d: Write BTP Capabilities Request to C1 ──
	hsReq := btpHandshakeRequest(btpSupportedVersionsList, btpDefaultATTMTU, btpDefaultWindowSize)
	slog.Debug("ble: sending BTP handshake request", "len", len(hsReq), "hex", hex.EncodeToString(hsReq))

	if n, err := c1.Write(hsReq); err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: sending BTP handshake request: %w", err)
	} else {
		slog.Debug("ble: BTP handshake request written", "bytesWritten", n)
	}

	// ── Step 5e: Poll for the BTP Capabilities Response (Path B) ──
	//
	// WaitForValue polls CBCharacteristic.value on bt_queue every 10 ms.
	// All reads are dispatched to bt_queue so we see CoreBluetooth's updates.
	// The value was pre-cleared above, so any non-nil result is fresh data.
	hsTimeout, hsCancel := context.WithTimeout(ctx, 15*time.Second)
	defer hsCancel()

	go func() {
		data, err := c2.WaitForValue(hsTimeout)
		if err != nil {
			slog.Debug("ble: C2 poll path ended", "err", err)
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

	// Wait for whichever path delivers the handshake response first.
	var hsData []byte
	select {
	case hsData = <-hsResp:
		hsCancel() // stop the poll goroutine
		slog.Debug("ble: BTP handshake response received")
	case <-hsTimeout.Done():
		device.Disconnect()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ble: BTP handshake cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("ble: BTP handshake timed out after 15s")
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

	// 7. Start the C2 data-polling goroutine.
	//
	// This goroutine replaces the tinygo notification callback as the delivery
	// path for all incoming BTP segments. It polls CBCharacteristic.value
	// atomically (read + clear in one bt_queue block) at 10 ms intervals so
	// we never miss an indication even when tinygo's pointer-based dispatch
	// fails.
	conn.startC2DataPoller()

	return conn, nil
}

// startC2DataPoller launches a background goroutine that continuously polls
// C2 for incoming BTP segments using the atomic ReadAndClearCachedValue
// operation. Each non-nil result is fed directly to btp.handleSegment,
// bypassing tinygo's unreliable notification callback dispatch entirely.
//
// The goroutine stops when conn.closed is closed (i.e. on BLEConn.Close).
// It is a no-op if c2 is nil (e.g. in unit tests that bypass DialBLE).
func (c *BLEConn) startC2DataPoller() {
	if c.c2 == nil {
		return
	}
	go func() {
		const pollInterval = 10 * time.Millisecond
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-c.closed:
				return
			case <-ticker.C:
				data := c.c2.ReadAndClearCachedValue()
				if data == nil {
					continue
				}
				slog.Debug("ble: C2 data received (data poller)", "len", len(data), "hex", hex.EncodeToString(data))
				// Ignore stale BTP handshake messages that might arrive
				// after the handshake has already completed.
				if isBTPCapabilitiesMessage(data) {
					slog.Debug("ble: data poller: ignoring stale BTP capabilities message")
					continue
				}
				if err := c.btp.handleSegment(data); err != nil {
					slog.Debug("ble: BTP segment error in data poller", "err", err)
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
		return fmt.Errorf("ble: connection closed")
	default:
	}

	segments := c.btp.segment(msg)
	for _, seg := range segments {
		if err := c.btp.waitCanSend(ctx); err != nil {
			return fmt.Errorf("ble: flow control: %w", err)
		}
		if _, err := c.c1.Write(seg); err != nil {
			return fmt.Errorf("ble: writing segment to C1: %w", err)
		}
		c.btp.markSent()
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
		return nil, nil, fmt.Errorf("ble: connection closed")
	}
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
