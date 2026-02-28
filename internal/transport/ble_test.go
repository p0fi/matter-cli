// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package transport

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── BLEAddr tests ────────────────────────────────────────────────────────────

func TestBLEAddr_Network(t *testing.T) {
	addr := &BLEAddr{Address: "AA:BB:CC:DD:EE:FF"}
	assert.Equal(t, "ble", addr.Network())
}

func TestBLEAddr_String(t *testing.T) {
	addr := &BLEAddr{Address: "AA:BB:CC:DD:EE:FF"}
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", addr.String())
}

// ─── DialBLE tests ────────────────────────────────────────────────────────────

// buildMockDeviceForDial constructs a mock BLE device that has the Matter
// service and C1/C2 characteristics ready for a BTP handshake.
func buildMockDeviceForDial() (*mockBLEAdapter, *mockBLECharacteristic, *mockBLECharacteristic) {
	c1 := &mockBLECharacteristic{uuid: MatterC1UUID}
	c2 := &mockBLECharacteristic{uuid: MatterC2UUID, waitCh: make(chan []byte, 1)}
	svc := &mockBLEService{
		uuid:            MatterServiceUUID,
		characteristics: []bleCharacteristic{c1, c2},
	}
	dev := &mockBLEDevice{
		services: []bleService{svc},
	}
	adapter := &mockBLEAdapter{
		connectFn: func(ctx context.Context, addr BLEAddress) (bleDevice, error) {
			return dev, nil
		},
	}
	return adapter, c1, c2
}

// buildBTPHandshakeResponse constructs a valid BTP Capabilities Response.
func buildBTPHandshakeResponse() []byte {
	resp := make([]byte, btpCapsResponseLen)
	resp[0] = btpCapsMagic1
	resp[1] = btpCapsMagic2
	resp[2] = btpCurrentVersion
	resp[3] = byte(btpDefaultSegmentSize)
	resp[4] = byte(btpDefaultSegmentSize >> 8)
	resp[5] = btpDefaultWindowSize
	return resp
}

func TestDialBLE_Success(t *testing.T) {
	adapter, _, c2 := buildMockDeviceForDial()

	// Simulate the device sending a BTP HandshakeResponse. The response is
	// delivered via the WaitForValue channel (poll path), which is used as
	// a backup during the handshake phase. The notification callback path
	// is also tested separately below.
	go func() {
		// Wait long enough for DialBLE to complete: WaitForNotifying (instant
		// in mock) + 50 ms settle sleep + ClearCachedValue (instant in mock) +
		// C1 Write (instant in mock). Using 200 ms gives a comfortable margin
		// so ClearCachedValue never races against our send and inadvertently
		// drains the response before WaitForValue picks it up.
		time.Sleep(200 * time.Millisecond)
		resp := buildBTPHandshakeResponse()
		c2.waitCh <- resp
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := DialBLE(ctx, adapter, "AA:BB:CC:DD:EE:FF")
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// Verify the peer address.
	assert.Equal(t, "ble", conn.peerAddr.Network())
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", conn.peerAddr.String())
}

func TestDialBLE_NoMatterService(t *testing.T) {
	// Device has no services.
	dev := &mockBLEDevice{services: nil}
	adapter := &mockBLEAdapter{
		connectFn: func(ctx context.Context, addr BLEAddress) (bleDevice, error) {
			return dev, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := DialBLE(ctx, adapter, "AA:BB:CC:DD:EE:FF")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDialBLE_MissingCharacteristics(t *testing.T) {
	// Service exists but has no characteristics.
	svc := &mockBLEService{
		uuid:            MatterServiceUUID,
		characteristics: nil,
	}
	dev := &mockBLEDevice{services: []bleService{svc}}
	adapter := &mockBLEAdapter{
		connectFn: func(ctx context.Context, addr BLEAddress) (bleDevice, error) {
			return dev, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := DialBLE(ctx, adapter, "AA:BB:CC:DD:EE:FF")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required characteristic")
}

func TestDialBLE_SuccessViaNotificationPath(t *testing.T) {
	adapter, _, c2 := buildMockDeviceForDial()

	// This test verifies that the notification callback path (Path A) also
	// works. We deliver the response only via the notification callback and
	// leave waitCh empty (drain it first so WaitForValue blocks).
	drainedC2 := &mockBLECharacteristic{uuid: MatterC2UUID, waitCh: nil}
	// Rebuild the adapter with drainedC2 so WaitForValue blocks forever.
	svc := &mockBLEService{
		uuid:            MatterServiceUUID,
		characteristics: []bleCharacteristic{&mockBLECharacteristic{uuid: MatterC1UUID}, drainedC2},
	}
	dev := &mockBLEDevice{services: []bleService{svc}}
	adapter = &mockBLEAdapter{
		connectFn: func(ctx context.Context, addr BLEAddress) (bleDevice, error) {
			return dev, nil
		},
	}
	_ = c2 // original c2 unused in this test

	go func() {
		// With the corrected handshake ordering (write C1 first, then
		// subscribe C2), EnableNotifications is called after the
		// WriteWithResponse + 50 ms settle sleep. We need to wait long
		// enough for the callback to be registered before invoking it.
		time.Sleep(200 * time.Millisecond)
		resp := buildBTPHandshakeResponse()
		if cb := drainedC2.notifCallback(); cb != nil {
			cb(resp)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := DialBLE(ctx, adapter, "AA:BB:CC:DD:EE:FF")
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	assert.Equal(t, "ble", conn.peerAddr.Network())
}

func TestDialBLE_HandshakeTimeout(t *testing.T) {
	adapter, _, _ := buildMockDeviceForDial()
	// Don't send a handshake response — the dial should time out.

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := DialBLE(ctx, adapter, "AA:BB:CC:DD:EE:FF")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BTP handshake")
}

// ─── BLEConn.Send/Receive tests ──────────────────────────────────────────────

// newTestBLEConn creates a BLEConn for testing with a mock C1 characteristic
// and a pre-initialized BTP session. The BTP session is pre-configured to
// skip the handshake. c2 is left nil so startDisconnectWatcher is a no-op —
// tests that need incoming segments inject them directly via btp.handleSegment.
func newTestBLEConn() (*BLEConn, *mockBLECharacteristic, *mockBLEDevice) {
	c1 := &mockBLECharacteristic{uuid: MatterC1UUID}
	dev := &mockBLEDevice{}
	btp := newBTPSession()
	btp.initHandshake(btpCurrentVersion, btpDefaultATTMTU, btpDefaultWindowSize)

	conn := &BLEConn{
		device:   dev,
		c1:       c1,
		c2:       nil, // no C2 in unit tests; data injected directly via btp.handleSegment
		btp:      btp,
		peerAddr: &BLEAddr{Address: "AA:BB:CC:DD:EE:FF"},
		closed:   make(chan struct{}),
	}
	return conn, c1, dev
}

func TestBLEConn_SendWritesToC1(t *testing.T) {
	conn, c1, _ := newTestBLEConn()
	defer conn.Close()

	ctx := context.Background()
	err := conn.Send(ctx, []byte("hello"), nil)
	require.NoError(t, err)

	written := c1.writtenData()
	require.NotEmpty(t, written, "expected at least one write to C1")
}

func TestBLEConn_ReceiveBlocksUntilMessage(t *testing.T) {
	conn, _, _ := newTestBLEConn()
	defer conn.Close()

	// Simulate a received message by delivering segments into the BTP session.
	msg := []byte("test-matter-message")
	segments := conn.btp.segment(msg)

	// Feed segments back as if received from C2.
	go func() {
		time.Sleep(20 * time.Millisecond)
		for _, seg := range segments {
			// Simulate the peer acking our segments.
			conn.btp.processAck(seg[2]) // ack the sequence number
			conn.btp.handleSegment(seg)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, addr, err := conn.Receive(ctx)
	require.NoError(t, err)
	assert.Equal(t, msg, data)
	assert.Equal(t, "ble", addr.Network())
}

func TestBLEConn_ReceiveContextCancelled(t *testing.T) {
	conn, _, _ := newTestBLEConn()
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := conn.Receive(ctx)
	require.Error(t, err)
}

func TestBLEConn_Close_DisconnectsDevice(t *testing.T) {
	conn, _, dev := newTestBLEConn()

	err := conn.Close()
	require.NoError(t, err)
	assert.True(t, dev.disconnectCalled, "device.Disconnect should be called on Close")
}

func TestBLEConn_Close_Idempotent(t *testing.T) {
	conn, _, _ := newTestBLEConn()

	// Close multiple times — should not panic.
	require.NoError(t, conn.Close())
	require.NoError(t, conn.Close())
}

func TestBLEConn_SendAfterClose(t *testing.T) {
	conn, _, _ := newTestBLEConn()
	conn.Close()

	err := conn.Send(context.Background(), []byte("data"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// ─── Disconnection detection ──────────────────────────────────────────────────

func TestBLEConn_DisconnectWatcherDetectsDisconnection(t *testing.T) {
	// Build a BLEConn with a mock C2 that we can mark as disconnected.
	c1 := &mockBLECharacteristic{uuid: MatterC1UUID}
	c2 := &mockBLECharacteristic{uuid: MatterC2UUID, waitCh: make(chan []byte, 1)}
	dev := &mockBLEDevice{}
	btp := newBTPSession()
	btp.initHandshake(btpCurrentVersion, btpDefaultATTMTU, btpDefaultWindowSize)

	conn := &BLEConn{
		device:   dev,
		c1:       c1,
		c2:       c2,
		btp:      btp,
		peerAddr: &BLEAddr{Address: "AA:BB:CC:DD:EE:FF"},
		closed:   make(chan struct{}),
	}

	// Start the disconnect watcher.
	conn.startDisconnectWatcher()

	// Simulate peripheral disconnection.
	c2.disconnected = true

	// The watcher checks connectivity every 1 s. Give it up to 3 seconds
	// to detect the disconnection and close the connection.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Receive should unblock with an error once the watcher closes the conn.
	_, _, err := conn.Receive(ctx)
	require.Error(t, err, "Receive should return an error after disconnection")
	assert.Contains(t, err.Error(), "closed",
		"error should indicate the connection was closed")

	// The device should have been disconnected.
	assert.True(t, dev.disconnectCalled,
		"device.Disconnect should be called when the watcher detects disconnection")
}

// ─── Compile-time interface check ─────────────────────────────────────────────

func TestBLEConn_ImplementsConn(t *testing.T) {
	var _ Conn = (*BLEConn)(nil)
}
