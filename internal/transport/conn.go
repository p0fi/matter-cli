// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package transport provides network transport abstractions for the Matter protocol,
// including UDP socket handling and the Message Reliability Protocol (MRP).
package transport

import (
	"context"
	"net"
)

// Conn abstracts a network connection for sending and receiving raw Matter messages.
// Implementations must be safe for concurrent use by multiple goroutines.
type Conn interface {
	// Send transmits msg to the given address.
	Send(ctx context.Context, msg []byte, addr net.Addr) error

	// Receive blocks until a message arrives or the context is cancelled.
	// It returns the raw message bytes and the sender's address.
	Receive(ctx context.Context) ([]byte, net.Addr, error)

	// Close shuts down the connection and releases associated resources.
	Close() error
}
