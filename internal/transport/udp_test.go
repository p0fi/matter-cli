// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestUDPConn_SendReceive(t *testing.T) {
	// Create two UDP connections.
	conn1, err := NewUDPConn("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewUDPConn 1: %v", err)
	}
	defer conn1.Close()

	conn2, err := NewUDPConn("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewUDPConn 2: %v", err)
	}
	defer conn2.Close()

	// Send from conn1 to conn2.
	msg := []byte("hello matter")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := conn1.Send(ctx, msg, conn2.LocalAddr()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	data, addr, err := conn2.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}

	if string(data) != string(msg) {
		t.Errorf("received %q, want %q", data, msg)
	}

	// Verify sender address matches conn1.
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		t.Fatalf("expected *net.UDPAddr, got %T", addr)
	}
	localAddr := conn1.LocalAddr().(*net.UDPAddr)
	if udpAddr.Port != localAddr.Port {
		t.Errorf("sender port: got %d, want %d", udpAddr.Port, localAddr.Port)
	}
}

func TestUDPConn_ReceiveCancelled(t *testing.T) {
	conn, err := NewUDPConn("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewUDPConn: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err = conn.Receive(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestUDPConn_SendCancelledContext(t *testing.T) {
	conn, err := NewUDPConn("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewUDPConn: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = conn.Send(ctx, []byte("test"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestUDPConn_SendInvalidAddr(t *testing.T) {
	conn, err := NewUDPConn("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewUDPConn: %v", err)
	}
	defer conn.Close()

	// Pass a non-UDP address.
	tcpAddr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
	err = conn.Send(context.Background(), []byte("test"), tcpAddr)
	if err == nil {
		t.Fatal("expected error for non-UDP address")
	}
}

func TestUDPConn_LocalAddr(t *testing.T) {
	conn, err := NewUDPConn("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewUDPConn: %v", err)
	}
	defer conn.Close()

	addr := conn.LocalAddr()
	if addr == nil {
		t.Fatal("LocalAddr returned nil")
	}

	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		t.Fatalf("expected *net.UDPAddr, got %T", addr)
	}
	if udpAddr.Port == 0 {
		t.Error("expected non-zero port")
	}
}

func TestUDPConn_MultipleMessages(t *testing.T) {
	conn1, err := NewUDPConn("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewUDPConn 1: %v", err)
	}
	defer conn1.Close()

	conn2, err := NewUDPConn("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewUDPConn 2: %v", err)
	}
	defer conn2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	messages := []string{"msg1", "msg2", "msg3"}
	for _, m := range messages {
		if err := conn1.Send(ctx, []byte(m), conn2.LocalAddr()); err != nil {
			t.Fatalf("Send %q: %v", m, err)
		}
	}

	for _, want := range messages {
		data, _, err := conn2.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		if string(data) != want {
			t.Errorf("received %q, want %q", data, want)
		}
	}
}

func TestNewUDPConnFromConn(t *testing.T) {
	udpAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}
	rawConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}

	conn := NewUDPConnFromConn(rawConn)
	defer conn.Close()

	if conn.LocalAddr() == nil {
		t.Error("LocalAddr returned nil")
	}
}
