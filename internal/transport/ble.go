// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package transport

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
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
	device, err := adapter.Connect(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("ble: connecting to %s: %w", addr, err)
	}

	// 2. Discover the Matter service.
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

	// 4. Set up BTP session.
	btp := newBTPSession()

	// 5. Subscribe to C2 indications and route data into the BTP session.
	if err := c2.EnableNotifications(func(data []byte) {
		if err := btp.handleSegment(data); err != nil {
			slog.Debug("ble: BTP segment error", "err", err)
		}
	}); err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: subscribing to C2: %w", err)
	}

	// 6. Send BTP HandshakeRequest via C1.
	hsReq := btpHandshakeRequest(btpSupportedVersions, btpDefaultATTMTU, btpDefaultWindowSize)
	if _, err := c1.Write(hsReq); err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: sending BTP handshake request: %w", err)
	}

	// 7. Await HandshakeResponse on BTP messages channel.
	// The handshake response arrives as an indication on C2, but since we
	// haven't set up the normal segment handler yet for handshake frames,
	// we read it from a temporary indication. For simplicity we handle it
	// via a second notification subscription isn't feasible, so we use a
	// channel-based approach.
	//
	// Actually, the C2 notification callback routes everything through
	// btp.handleSegment which rejects handshake frames. Instead, we
	// temporarily subscribe with a handshake-specific handler.
	//
	// Re-subscribe C2 with a two-phase approach: first phase catches the
	// handshake response, then switches to normal data handling.
	hsResp := make(chan []byte, 1)
	if err := c2.EnableNotifications(func(data []byte) {
		if len(data) > 0 && data[0]&btpFlagHandshake != 0 {
			select {
			case hsResp <- data:
			default:
			}
			return
		}
		// Normal BTP data segment.
		if err := btp.handleSegment(data); err != nil {
			slog.Debug("ble: BTP segment error", "err", err)
		}
	}); err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: re-subscribing to C2 for handshake: %w", err)
	}

	// Re-send the handshake request since we changed the notification handler.
	if _, err := c1.Write(hsReq); err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: re-sending BTP handshake request: %w", err)
	}

	// Wait for the handshake response.
	var hsData []byte
	select {
	case hsData = <-hsResp:
	case <-ctx.Done():
		device.Disconnect()
		return nil, fmt.Errorf("ble: BTP handshake timed out: %w", ctx.Err())
	}

	version, attMTU, windowSize, err := parseBTPHandshakeResponse(hsData)
	if err != nil {
		device.Disconnect()
		return nil, fmt.Errorf("ble: %w", err)
	}

	// 8. Apply negotiated parameters.
	btp.initHandshake(version, attMTU, windowSize)

	conn := &BLEConn{
		device:   device,
		c1:       c1,
		btp:      btp,
		peerAddr: &BLEAddr{Address: addr},
		closed:   make(chan struct{}),
	}

	return conn, nil
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
