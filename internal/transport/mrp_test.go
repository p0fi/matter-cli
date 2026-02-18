// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

// mockConn is a test implementation of Conn that records sent messages.
type mockConn struct {
	mu       sync.Mutex
	sent     []sentMessage
	recvChan chan recvResult
}

type sentMessage struct {
	data []byte
	addr net.Addr
}

type recvResult struct {
	data []byte
	addr net.Addr
	err  error
}

func newMockConn() *mockConn {
	return &mockConn{
		recvChan: make(chan recvResult, 10),
	}
}

func (m *mockConn) Send(_ context.Context, msg []byte, addr net.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(msg))
	copy(cp, msg)
	m.sent = append(m.sent, sentMessage{data: cp, addr: addr})
	return nil
}

func (m *mockConn) Receive(ctx context.Context) ([]byte, net.Addr, error) {
	select {
	case r := <-m.recvChan:
		return r.data, r.addr, r.err
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func (m *mockConn) Close() error {
	return nil
}

func (m *mockConn) sentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

func TestMRP_SendReliableWithACK(t *testing.T) {
	mc := newMockConn()
	config := MRPConfig{
		IdleRetransmitTimeout:   50 * time.Millisecond,
		ActiveRetransmitTimeout: 30 * time.Millisecond,
		MaxRetransmits:          3,
	}
	mrp := NewMRP(mc, config)
	defer mrp.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}
	msg := []byte("reliable message")
	counter := uint32(42)

	// Send in a goroutine, then ACK quickly.
	done := make(chan error, 1)
	go func() {
		done <- mrp.SendReliable(ctx, msg, addr, counter, 1, false)
	}()

	// Allow initial send to happen.
	time.Sleep(10 * time.Millisecond)

	// ACK the message.
	if !mrp.HandleACK(counter) {
		t.Error("HandleACK returned false")
	}

	if err := <-done; err != nil {
		t.Fatalf("SendReliable: %v", err)
	}

	if mrp.PendingCount() != 0 {
		t.Errorf("PendingCount: got %d, want 0", mrp.PendingCount())
	}
}

func TestMRP_RetransmitsOnTimeout(t *testing.T) {
	mc := newMockConn()
	config := MRPConfig{
		IdleRetransmitTimeout:   30 * time.Millisecond,
		ActiveRetransmitTimeout: 30 * time.Millisecond,
		MaxRetransmits:          2,
	}
	mrp := NewMRP(mc, config)
	defer mrp.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}
	msg := []byte("will retry")
	counter := uint32(1)

	done := make(chan error, 1)
	go func() {
		done <- mrp.SendReliable(ctx, msg, addr, counter, 1, false)
	}()

	// Wait long enough for initial send + retransmits + max exceeded.
	// With 30ms timeout and 2 max retransmits: initial + 2 retransmits + close.
	time.Sleep(200 * time.Millisecond)

	// The pending entry should have been cleaned up by now (max retransmits exceeded).
	// The channel should have been closed, causing SendReliable to return.
	select {
	case <-done:
		// OK - SendReliable returned.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SendReliable did not return after max retransmits")
	}

	// Should have sent: 1 initial + up to 2 retransmits = 3 total.
	count := mc.sentCount()
	if count < 2 || count > 3 {
		t.Errorf("sent count: got %d, want 2-3", count)
	}
}

func TestMRP_ActiveRetransmitTimeout(t *testing.T) {
	mc := newMockConn()
	config := MRPConfig{
		IdleRetransmitTimeout:   500 * time.Millisecond,
		ActiveRetransmitTimeout: 30 * time.Millisecond,
		MaxRetransmits:          1,
	}
	mrp := NewMRP(mc, config)
	defer mrp.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}
	counter := uint32(10)

	done := make(chan error, 1)
	go func() {
		done <- mrp.SendReliable(ctx, []byte("active"), addr, counter, 1, true)
	}()

	// With active timeout of 30ms, retransmit should happen quickly.
	time.Sleep(100 * time.Millisecond)

	select {
	case <-done:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SendReliable did not complete with active timeout")
	}

	// Should have at least 2 sends (initial + 1 retransmit).
	count := mc.sentCount()
	if count < 2 {
		t.Errorf("sent count: got %d, want >= 2", count)
	}
}

func TestMRP_HandleACKUnknown(t *testing.T) {
	mc := newMockConn()
	mrp := NewMRP(mc, DefaultMRPConfig())
	defer mrp.Close()

	if mrp.HandleACK(99999) {
		t.Error("HandleACK should return false for unknown counter")
	}
}

func TestMRP_ContextCancellation(t *testing.T) {
	mc := newMockConn()
	config := MRPConfig{
		IdleRetransmitTimeout:   1 * time.Second,
		ActiveRetransmitTimeout: 1 * time.Second,
		MaxRetransmits:          10,
	}
	mrp := NewMRP(mc, config)
	defer mrp.Close()

	ctx, cancel := context.WithCancel(context.Background())
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}

	done := make(chan error, 1)
	go func() {
		done <- mrp.SendReliable(ctx, []byte("cancel me"), addr, 1, 1, false)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	err := <-done
	if err == nil {
		t.Fatal("expected error after context cancellation")
	}
}

func TestMRP_Close(t *testing.T) {
	mc := newMockConn()
	config := MRPConfig{
		IdleRetransmitTimeout:   1 * time.Second,
		ActiveRetransmitTimeout: 1 * time.Second,
		MaxRetransmits:          10,
	}
	mrp := NewMRP(mc, config)

	ctx := context.Background()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}

	done := make(chan error, 1)
	go func() {
		done <- mrp.SendReliable(ctx, []byte("close me"), addr, 1, 1, false)
	}()

	time.Sleep(20 * time.Millisecond)
	mrp.Close()

	err := <-done
	if err == nil {
		t.Fatal("expected error after MRP close")
	}

	// Close is idempotent.
	mrp.Close()
}

func TestMRP_PendingCount(t *testing.T) {
	mc := newMockConn()
	config := MRPConfig{
		IdleRetransmitTimeout:   1 * time.Second,
		ActiveRetransmitTimeout: 1 * time.Second,
		MaxRetransmits:          10,
	}
	mrp := NewMRP(mc, config)
	defer mrp.Close()

	ctx := context.Background()
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}

	for i := uint32(0); i < 5; i++ {
		go func(counter uint32) {
			_ = mrp.SendReliable(ctx, []byte(fmt.Sprintf("msg%d", counter)), addr, counter, 1, false)
		}(i)
	}

	time.Sleep(50 * time.Millisecond)

	if mrp.PendingCount() != 5 {
		t.Errorf("PendingCount: got %d, want 5", mrp.PendingCount())
	}

	// ACK two.
	mrp.HandleACK(0)
	mrp.HandleACK(2)

	if mrp.PendingCount() != 3 {
		t.Errorf("PendingCount after ACKs: got %d, want 3", mrp.PendingCount())
	}
}

func TestDefaultMRPConfig(t *testing.T) {
	config := DefaultMRPConfig()
	if config.IdleRetransmitTimeout != DefaultIdleRetransmitTimeout {
		t.Errorf("IdleRetransmitTimeout: got %v, want %v", config.IdleRetransmitTimeout, DefaultIdleRetransmitTimeout)
	}
	if config.ActiveRetransmitTimeout != DefaultActiveRetransmitTimeout {
		t.Errorf("ActiveRetransmitTimeout: got %v, want %v", config.ActiveRetransmitTimeout, DefaultActiveRetransmitTimeout)
	}
	if config.MaxRetransmits != DefaultMaxRetransmits {
		t.Errorf("MaxRetransmits: got %d, want %d", config.MaxRetransmits, DefaultMaxRetransmits)
	}
}
