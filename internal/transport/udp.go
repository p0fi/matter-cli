// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"context"
	"fmt"
	"net"
	"sync"
)

// maxMatterMessageSize is the maximum size of a Matter message over UDP.
const maxMatterMessageSize = 1280

// UDPConn implements Conn using a UDP socket.
type UDPConn struct {
	conn   *net.UDPConn
	closed chan struct{}
	once   sync.Once
}

// NewUDPConn creates a new UDP connection bound to the given local address.
// Pass an empty address (e.g., ":0") to let the OS pick a port.
func NewUDPConn(addr string) (*UDPConn, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("transport: resolve UDP address %q: %w", addr, err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("transport: listen UDP on %s: %w", addr, err)
	}
	return &UDPConn{
		conn:   conn,
		closed: make(chan struct{}),
	}, nil
}

// NewUDPConnFromConn wraps an existing *net.UDPConn as a transport.Conn.
func NewUDPConnFromConn(conn *net.UDPConn) *UDPConn {
	return &UDPConn{
		conn:   conn,
		closed: make(chan struct{}),
	}
}

// Send transmits msg to the specified address via UDP.
func (u *UDPConn) Send(ctx context.Context, msg []byte, addr net.Addr) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return fmt.Errorf("transport: expected *net.UDPAddr, got %T", addr)
	}

	_, err := u.conn.WriteToUDP(msg, udpAddr)
	if err != nil {
		return fmt.Errorf("transport: UDP send: %w", err)
	}
	return nil
}

// Receive blocks until a UDP message is received or the context is cancelled.
func (u *UDPConn) Receive(ctx context.Context) ([]byte, net.Addr, error) {
	// Use a goroutine to make ReadFromUDP cancellable via context.
	type result struct {
		data []byte
		addr net.Addr
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, maxMatterMessageSize)
		n, addr, err := u.conn.ReadFromUDP(buf)
		if err != nil {
			ch <- result{err: err}
			return
		}
		// Copy only the received bytes.
		data := make([]byte, n)
		copy(data, buf[:n])
		ch <- result{data: data, addr: addr}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return nil, nil, fmt.Errorf("transport: UDP receive: %w", r.err)
		}
		return r.data, r.addr, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-u.closed:
		return nil, nil, fmt.Errorf("transport: %w", ErrConnClosed)
	}
}

// Close shuts down the UDP connection.
func (u *UDPConn) Close() error {
	u.once.Do(func() {
		close(u.closed)
	})
	return u.conn.Close()
}

// LocalAddr returns the local address of the UDP connection.
func (u *UDPConn) LocalAddr() net.Addr {
	return u.conn.LocalAddr()
}
