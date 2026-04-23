// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// MRP default timeout values as specified by the Matter specification.
const (
	// DefaultIdleRetransmitTimeout is the retransmit interval for idle exchanges.
	DefaultIdleRetransmitTimeout = 500 * time.Millisecond
	// DefaultActiveRetransmitTimeout is the retransmit interval for active exchanges.
	DefaultActiveRetransmitTimeout = 300 * time.Millisecond
	// DefaultMaxRetransmits is the maximum number of retransmission attempts.
	DefaultMaxRetransmits = 4
)

// MRPConfig holds configurable parameters for the Message Reliability Protocol.
type MRPConfig struct {
	// IdleRetransmitTimeout is the retransmit interval when the exchange is idle.
	IdleRetransmitTimeout time.Duration
	// ActiveRetransmitTimeout is the retransmit interval when the exchange is active.
	ActiveRetransmitTimeout time.Duration
	// MaxRetransmits is the maximum number of retransmission attempts.
	MaxRetransmits int
}

// DefaultMRPConfig returns the default MRP configuration.
func DefaultMRPConfig() MRPConfig {
	return MRPConfig{
		IdleRetransmitTimeout:   DefaultIdleRetransmitTimeout,
		ActiveRetransmitTimeout: DefaultActiveRetransmitTimeout,
		MaxRetransmits:          DefaultMaxRetransmits,
	}
}

// MRP implements the Matter Message Reliability Protocol on top of a Conn.
// It handles retransmission of unacknowledged messages and ACK tracking.
type MRP struct {
	conn   Conn
	config MRPConfig

	mu        sync.Mutex
	pending   map[uint32]*pendingMessage
	closed    chan struct{}
	closeOnce sync.Once
}

// pendingMessage tracks a message awaiting acknowledgment.
type pendingMessage struct {
	data       []byte
	addr       net.Addr
	retries    int
	timer      *time.Timer
	acked      chan struct{}
	exchangeID uint16
}

// NewMRP creates a new MRP layer on top of the given connection.
func NewMRP(conn Conn, config MRPConfig) *MRP {
	return &MRP{
		conn:    conn,
		config:  config,
		pending: make(map[uint32]*pendingMessage),
		closed:  make(chan struct{}),
	}
}

// SendReliable sends a message and tracks it for retransmission until an ACK
// is received or the maximum retransmit count is exceeded. The messageCounter
// is used as the key for matching ACKs. The exchangeActive flag determines
// which retransmit timeout to use.
func (m *MRP) SendReliable(ctx context.Context, msg []byte, addr net.Addr, messageCounter uint32, exchangeID uint16, exchangeActive bool) error {
	if err := m.conn.Send(ctx, msg, addr); err != nil {
		return fmt.Errorf("transport: MRP initial send: %w", err)
	}

	acked := make(chan struct{})
	pm := &pendingMessage{
		data:       msg,
		addr:       addr,
		retries:    0,
		acked:      acked,
		exchangeID: exchangeID,
	}

	timeout := m.config.IdleRetransmitTimeout
	if exchangeActive {
		timeout = m.config.ActiveRetransmitTimeout
	}

	m.mu.Lock()
	m.pending[messageCounter] = pm
	pm.timer = time.AfterFunc(timeout, func() {
		m.retransmit(ctx, messageCounter, timeout)
	})
	m.mu.Unlock()

	// Wait for ACK, context cancellation, or MRP shutdown.
	select {
	case <-acked:
		return nil
	case <-ctx.Done():
		m.removePending(messageCounter)
		return ctx.Err()
	case <-m.closed:
		m.removePending(messageCounter)
		return fmt.Errorf("transport: MRP closed")
	}
}

// retransmit handles a retransmission timeout for the given message counter.
func (m *MRP) retransmit(ctx context.Context, messageCounter uint32, timeout time.Duration) {
	m.mu.Lock()
	pm, ok := m.pending[messageCounter]
	if !ok {
		m.mu.Unlock()
		return
	}

	pm.retries++
	if pm.retries > m.config.MaxRetransmits {
		// Max retransmits exceeded; signal failure.
		delete(m.pending, messageCounter)
		m.mu.Unlock()
		close(pm.acked)
		return
	}

	// Reschedule the timer.
	pm.timer.Reset(timeout)
	data := pm.data
	addr := pm.addr
	m.mu.Unlock()

	// Best-effort retransmit; errors are not fatal to the MRP layer.
	_ = m.conn.Send(ctx, data, addr)
}

// HandleACK marks a message as acknowledged, stopping any pending retransmissions.
func (m *MRP) HandleACK(ackMessageCounter uint32) bool {
	m.mu.Lock()
	pm, ok := m.pending[ackMessageCounter]
	if !ok {
		m.mu.Unlock()
		return false
	}
	pm.timer.Stop()
	delete(m.pending, ackMessageCounter)
	m.mu.Unlock()

	close(pm.acked)
	return true
}

// removePending removes and cleans up a pending message entry.
func (m *MRP) removePending(messageCounter uint32) {
	m.mu.Lock()
	pm, ok := m.pending[messageCounter]
	if ok {
		pm.timer.Stop()
		delete(m.pending, messageCounter)
	}
	m.mu.Unlock()
}

// PendingCount returns the number of messages currently awaiting acknowledgment.
func (m *MRP) PendingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending)
}

// Close shuts down the MRP layer and stops all pending retransmissions.
func (m *MRP) Close() error {
	m.closeOnce.Do(func() {
		close(m.closed)

		m.mu.Lock()
		for counter, pm := range m.pending {
			pm.timer.Stop()
			delete(m.pending, counter)
		}
		m.mu.Unlock()
	})
	return nil
}
