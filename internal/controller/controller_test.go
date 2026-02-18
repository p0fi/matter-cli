// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/secure"
)

// mockAddr implements net.Addr for testing.
type mockAddr struct {
	network string
	address string
}

func (a *mockAddr) Network() string { return a.network }
func (a *mockAddr) String() string  { return a.address }

// pipeConn is an in-memory transport.Conn that connects two ends via channels.
type pipeConn struct {
	send    chan pipeMsg
	recv    chan pipeMsg
	closed  chan struct{}
	once    sync.Once
	myAddr  net.Addr
}

type pipeMsg struct {
	data []byte
	addr net.Addr
}

func newPipePair() (*pipeConn, *pipeConn) {
	ch1 := make(chan pipeMsg, 16)
	ch2 := make(chan pipeMsg, 16)
	addrA := &mockAddr{network: "pipe", address: "sideA"}
	addrB := &mockAddr{network: "pipe", address: "sideB"}
	a := &pipeConn{send: ch1, recv: ch2, closed: make(chan struct{}), myAddr: addrA}
	b := &pipeConn{send: ch2, recv: ch1, closed: make(chan struct{}), myAddr: addrB}
	return a, b
}

func (p *pipeConn) Send(_ context.Context, msg []byte, addr net.Addr) error {
	data := make([]byte, len(msg))
	copy(data, msg)
	select {
	case p.send <- pipeMsg{data: data, addr: p.myAddr}:
		return nil
	case <-p.closed:
		return fmt.Errorf("pipe closed")
	}
}

func (p *pipeConn) Receive(ctx context.Context) ([]byte, net.Addr, error) {
	select {
	case msg := <-p.recv:
		return msg.data, msg.addr, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-p.closed:
		return nil, nil, fmt.Errorf("pipe closed")
	}
}

func (p *pipeConn) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

// TestSendMessage verifies that sendMessage encodes and transmits through the pipe.
func TestSendMessage(t *testing.T) {
	connA, connB := newPipePair()
	defer connA.Close()
	defer connB.Close()

	ctrl, err := NewWithConn(Config{FabricID: 1}, connA)
	if err != nil {
		t.Fatal(err)
	}
	ctrl.mu.Lock()
	ctrl.peerAddr = &mockAddr{network: "pipe", address: "peer"}
	ctrl.mu.Unlock()

	msg := &protocol.Message{
		Header: protocol.MessageHeader{SessionID: 0},
		Protocol: protocol.ProtocolHeader{
			ProtocolOpcode: 0x20,
			ProtocolID:     0x0000,
			ExchangeFlags:  protocol.ExFlagInitiator | protocol.ExFlagReliable,
		},
		Payload: []byte("hello"),
	}

	ctx := context.Background()
	if err := ctrl.sendMessage(ctx, msg); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}

	// Read from the other side.
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	data, _, err := connB.Receive(readCtx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("received empty message")
	}

	// Decode and verify.
	codec := protocol.NewCodec()
	decoded, err := codec.Decode(data, nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(decoded.Payload) != "hello" {
		t.Errorf("payload = %q, want %q", decoded.Payload, "hello")
	}
}

// TestMessagePumpRouting verifies that the message pump delivers messages to exchanges.
func TestMessagePumpRouting(t *testing.T) {
	connA, connB := newPipePair()
	defer connA.Close()
	defer connB.Close()

	ctrl, err := NewWithConn(Config{FabricID: 1}, connA)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create an exchange.
	unsecured := ctrl.sessions.UnsecuredSession()
	exchange, err := ctrl.exchanges.NewExchange(ctx, unsecured)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.exchanges.CloseExchange(exchange)

	// Start the message pump.
	ctrl.done = make(chan struct{})
	go ctrl.runMessagePump(ctx)

	// Send a response message from the "device" side that targets our exchange.
	responseMsg := &protocol.Message{
		Header: protocol.MessageHeader{
			SessionID:      0,
			MessageCounter: 1,
		},
		Protocol: protocol.ProtocolHeader{
			ProtocolOpcode: 0x21, // PBKDFParamResponse
			ProtocolID:     0x0000,
			ExchangeID:     exchange.ID,
			ExchangeFlags:  0, // not initiator (response from device)
		},
		Payload: []byte("response-data"),
	}

	codec := protocol.NewCodec()
	encoded, err := codec.Encode(responseMsg, nil)
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}

	// Send it through the pipe (simulating device -> controller).
	if err := connB.Send(ctx, encoded, connB.myAddr); err != nil {
		t.Fatalf("pipe send: %v", err)
	}

	// The exchange should receive it.
	recvCtx, recvCancel := context.WithTimeout(ctx, 2*time.Second)
	defer recvCancel()

	msg, err := exchange.Receive(recvCtx)
	if err != nil {
		t.Fatalf("exchange.Receive: %v", err)
	}

	if string(msg.Payload) != "response-data" {
		t.Errorf("payload = %q, want %q", msg.Payload, "response-data")
	}
}

// TestConnectPASE performs a full PASE handshake between a controller and a
// simulated device (PASEResponder) connected via an in-memory pipe.
func TestConnectPASE(t *testing.T) {
	connA, connB := newPipePair()
	defer connA.Close()
	defer connB.Close()

	ctrl, err := NewWithConn(Config{FabricID: 1}, connA)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	passcode := uint32(20202021)
	salt := []byte("test-salt-16byte")
	iterations := uint32(1000)

	// Run a simulated PASE responder on the device side.
	deviceDone := make(chan error, 1)
	go func() {
		deviceDone <- runDevicePASEResponder(connB, passcode, salt, iterations)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set the peer address to the mock addr and use connectPASEDirect which
	// skips DNS resolution (since we're using a pipe, not real UDP).
	ctrl.mu.Lock()
	ctrl.peerAddr = connB.myAddr
	ctrl.mu.Unlock()

	session, err := connectPASEDirect(ctx, ctrl, passcode)
	if err != nil {
		// Check if the device had an error too.
		select {
		case devErr := <-deviceDone:
			if devErr != nil {
				t.Logf("device error: %v", devErr)
			}
		default:
		}
		t.Fatalf("ConnectPASE: %v", err)
	}

	if session == nil {
		t.Fatal("session is nil")
	}
	if session.Type != protocol.SessionPASE {
		t.Errorf("session type = %v, want PASE", session.Type)
	}
	if len(session.EncryptKey) != 16 {
		t.Errorf("encrypt key length = %d, want 16", len(session.EncryptKey))
	}
	if len(session.DecryptKey) != 16 {
		t.Errorf("decrypt key length = %d, want 16", len(session.DecryptKey))
	}

	// Wait for device side to finish.
	if err := <-deviceDone; err != nil {
		t.Errorf("device responder error: %v", err)
	}
}

// connectPASEDirect performs the PASE handshake without DNS resolution.
// Used for testing with mock transports where peer address is pre-set.
func connectPASEDirect(ctx context.Context, ctrl *Controller, passcode uint32) (*protocol.Session, error) {
	return ctrl.connectPASEWithAddr(ctx, passcode)
}

// runDevicePASEResponder simulates a Matter device running the PASE responder
// side of the handshake. It reads and writes raw encoded messages on the pipe.
func runDevicePASEResponder(conn *pipeConn, passcode uint32, salt []byte, iterations uint32) error {
	codec := protocol.NewCodec()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	responder := secure.NewPASEResponder(passcode, salt, iterations, 100)

	// Step 1: Receive PBKDFParamRequest.
	data, _, err := conn.Receive(ctx)
	if err != nil {
		return fmt.Errorf("receiving PBKDFParamRequest: %w", err)
	}
	msg, err := codec.Decode(data, nil)
	if err != nil {
		return fmt.Errorf("decoding PBKDFParamRequest: %w", err)
	}
	if msg.Protocol.ProtocolOpcode != secure.OpcodePBKDFParamRequest {
		return fmt.Errorf("expected PBKDFParamRequest (0x%02x), got 0x%02x",
			secure.OpcodePBKDFParamRequest, msg.Protocol.ProtocolOpcode)
	}

	respBytes, err := responder.ProcessPBKDFParamRequest(msg.Payload)
	if err != nil {
		return fmt.Errorf("processing PBKDFParamRequest: %w", err)
	}

	// Send PBKDFParamResponse.
	respMsg := &protocol.Message{
		Header: protocol.MessageHeader{SessionID: 0, MessageCounter: 1},
		Protocol: protocol.ProtocolHeader{
			ProtocolOpcode: secure.OpcodePBKDFParamResponse,
			ProtocolID:     secure.ProtocolIDSecureChannel,
			ExchangeID:     msg.Protocol.ExchangeID,
			ExchangeFlags:  0, // responder, not initiator
		},
		Payload: respBytes,
	}
	encoded, err := codec.Encode(respMsg, nil)
	if err != nil {
		return fmt.Errorf("encoding PBKDFParamResponse: %w", err)
	}
	if err := conn.Send(ctx, encoded, conn.myAddr); err != nil {
		return fmt.Errorf("sending PBKDFParamResponse: %w", err)
	}

	// Step 2: Receive PAKE1.
	data, _, err = conn.Receive(ctx)
	if err != nil {
		return fmt.Errorf("receiving PAKE1: %w", err)
	}
	msg, err = codec.Decode(data, nil)
	if err != nil {
		return fmt.Errorf("decoding PAKE1: %w", err)
	}
	if msg.Protocol.ProtocolOpcode != secure.OpcodePASEPake1 {
		return fmt.Errorf("expected PAKE1 (0x%02x), got 0x%02x",
			secure.OpcodePASEPake1, msg.Protocol.ProtocolOpcode)
	}

	pake2Bytes, err := responder.ProcessPAKE1(msg.Payload)
	if err != nil {
		return fmt.Errorf("processing PAKE1: %w", err)
	}

	// Send PAKE2.
	pake2Msg := &protocol.Message{
		Header: protocol.MessageHeader{SessionID: 0, MessageCounter: 2},
		Protocol: protocol.ProtocolHeader{
			ProtocolOpcode: secure.OpcodePASEPake2,
			ProtocolID:     secure.ProtocolIDSecureChannel,
			ExchangeID:     msg.Protocol.ExchangeID,
			ExchangeFlags:  0,
		},
		Payload: pake2Bytes,
	}
	encoded, err = codec.Encode(pake2Msg, nil)
	if err != nil {
		return fmt.Errorf("encoding PAKE2: %w", err)
	}
	if err := conn.Send(ctx, encoded, conn.myAddr); err != nil {
		return fmt.Errorf("sending PAKE2: %w", err)
	}

	// Step 3: Receive PAKE3.
	data, _, err = conn.Receive(ctx)
	if err != nil {
		return fmt.Errorf("receiving PAKE3: %w", err)
	}
	msg, err = codec.Decode(data, nil)
	if err != nil {
		return fmt.Errorf("decoding PAKE3: %w", err)
	}
	if msg.Protocol.ProtocolOpcode != secure.OpcodePASEPake3 {
		return fmt.Errorf("expected PAKE3 (0x%02x), got 0x%02x",
			secure.OpcodePASEPake3, msg.Protocol.ProtocolOpcode)
	}

	if err := responder.ProcessPAKE3(msg.Payload); err != nil {
		return fmt.Errorf("processing PAKE3: %w", err)
	}

	// Send StatusReport (success).
	statusMsg := &protocol.Message{
		Header: protocol.MessageHeader{SessionID: 0, MessageCounter: 3},
		Protocol: protocol.ProtocolHeader{
			ProtocolOpcode: secure.OpcodeStatusReport,
			ProtocolID:     secure.ProtocolIDSecureChannel,
			ExchangeID:     msg.Protocol.ExchangeID,
			ExchangeFlags:  0,
		},
		Payload: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // success status
	}
	encoded, err = codec.Encode(statusMsg, nil)
	if err != nil {
		return fmt.Errorf("encoding StatusReport: %w", err)
	}
	if err := conn.Send(ctx, encoded, conn.myAddr); err != nil {
		return fmt.Errorf("sending StatusReport: %w", err)
	}

	return nil
}

// TestExchangeSendStampsFields verifies that Exchange.Send stamps the exchange
// ID and initiator flag correctly.
func TestExchangeSendStampsFields(t *testing.T) {
	var sent *protocol.Message
	sendFunc := func(_ context.Context, msg *protocol.Message) error {
		sent = msg
		return nil
	}

	exchange := &protocol.Exchange{
		ID:          42,
		Session:     &protocol.Session{ID: 7},
		IsInitiator: true,
		SendFunc:    sendFunc,
	}

	msg := &protocol.Message{
		Protocol: protocol.ProtocolHeader{
			ProtocolOpcode: 0x20,
			ProtocolID:     0x0000,
		},
	}

	if err := exchange.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if sent == nil {
		t.Fatal("message was not sent")
	}
	if sent.Protocol.ExchangeID != 42 {
		t.Errorf("ExchangeID = %d, want 42", sent.Protocol.ExchangeID)
	}
	if sent.Protocol.ExchangeFlags&protocol.ExFlagInitiator == 0 {
		t.Error("ExFlagInitiator not set")
	}
	if sent.Header.SessionID != 7 {
		t.Errorf("SessionID = %d, want 7", sent.Header.SessionID)
	}
}

// TestExchangeSendNilFunc verifies that Send is a no-op when SendFunc is nil.
func TestExchangeSendNilFunc(t *testing.T) {
	exchange := &protocol.Exchange{
		ID:          1,
		Session:     &protocol.Session{ID: 0},
		IsInitiator: true,
	}

	msg := &protocol.Message{}
	if err := exchange.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send with nil func should return nil, got: %v", err)
	}
}

// TestStaticDiscoverer verifies the static discoverer adapter.
func TestStaticDiscoverer(t *testing.T) {
	d := &StaticDiscoverer{Addr: "192.168.1.100:5540"}
	addr, err := d.DiscoverCommissionable(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "192.168.1.100:5540" {
		t.Errorf("addr = %q, want %q", addr, "192.168.1.100:5540")
	}
}
