// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package transport provides network transport abstractions for the Matter protocol,
// including UDP socket handling and the Message Reliability Protocol (MRP).
package transport

import (
	"context"
	"errors"
	"net"
)

// ErrConnClosed is returned by Conn methods when the connection has been
// permanently closed. Callers (such as the controller message pump) should
// treat this as a fatal condition and stop retrying rather than looping.
var ErrConnClosed = errors.New("connection closed")

// Conn abstracts a network connection for sending and receiving raw Matter messages.
// Implementations must be safe for concurrent use by multiple goroutines.
type Conn interface {
	// Send transmits msg to the given address.
	Send(ctx context.Context, msg []byte, addr net.Addr) error

	// Receive blocks until a message arrives or the context is cancelled.
	// It returns the raw message bytes and the sender's address.
	// When the connection has been permanently closed, implementations
	// must return an error wrapping ErrConnClosed.
	Receive(ctx context.Context) ([]byte, net.Addr, error)

	// Close shuts down the connection and releases associated resources.
	Close() error
}
