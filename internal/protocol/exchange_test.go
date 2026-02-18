// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestExchangeManager_NewExchange(t *testing.T) {
	em := NewExchangeManager()
	session := &Session{ID: 1, Type: SessionPASE}

	e, err := em.NewExchange(context.Background(), session)
	if err != nil {
		t.Fatalf("NewExchange: %v", err)
	}
	if e.ID != 0 {
		t.Errorf("first exchange ID: got %d, want 0", e.ID)
	}
	if !e.IsInitiator {
		t.Error("expected IsInitiator to be true")
	}
	if e.Session != session {
		t.Error("session mismatch")
	}
	if em.Count() != 1 {
		t.Errorf("Count: got %d, want 1", em.Count())
	}
}

func TestExchangeManager_NewExchangeCancelledContext(t *testing.T) {
	em := NewExchangeManager()
	session := &Session{ID: 1}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := em.NewExchange(ctx, session)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestExchangeManager_RouteToExistingExchange(t *testing.T) {
	em := NewExchangeManager()
	session := &Session{ID: 5, Type: SessionPASE}

	e, err := em.NewExchange(context.Background(), session)
	if err != nil {
		t.Fatalf("NewExchange: %v", err)
	}

	// Simulate a response from the peer (not initiator, so it matches our local exchange).
	msg := &Message{
		Header: MessageHeader{SessionID: 5},
		Protocol: ProtocolHeader{
			ExchangeFlags: 0, // not initiator
			ExchangeID:    e.ID,
		},
	}

	if err := em.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	received, err := e.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if received != msg {
		t.Error("received different message")
	}
}

func TestExchangeManager_IncomingNewExchange(t *testing.T) {
	em := NewExchangeManager()

	var handledExchange *Exchange
	var handledMsg *Message
	var mu sync.Mutex

	em.OnUnhandled = func(ctx context.Context, exchange *Exchange, msg *Message) error {
		mu.Lock()
		handledExchange = exchange
		handledMsg = msg
		mu.Unlock()
		return nil
	}

	// Simulate an incoming message from an initiator with no matching exchange.
	msg := &Message{
		Header: MessageHeader{SessionID: 10},
		Protocol: ProtocolHeader{
			ExchangeFlags: ExFlagInitiator,
			ExchangeID:    42,
		},
	}

	if err := em.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if handledExchange == nil {
		t.Fatal("OnUnhandled was not called")
	}
	if handledExchange.ID != 42 {
		t.Errorf("exchange ID: got %d, want 42", handledExchange.ID)
	}
	if handledExchange.IsInitiator {
		t.Error("expected IsInitiator to be false for received exchange")
	}
	if handledMsg != msg {
		t.Error("wrong message passed to handler")
	}
}

func TestExchangeManager_NoHandlerDrops(t *testing.T) {
	em := NewExchangeManager()

	// Incoming initiator message with no handler set.
	msg := &Message{
		Header: MessageHeader{SessionID: 1},
		Protocol: ProtocolHeader{
			ExchangeFlags: ExFlagInitiator,
			ExchangeID:    1,
		},
	}

	// Should not error; silently drops.
	if err := em.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
}

func TestExchangeManager_NoExchangeNoInitiator(t *testing.T) {
	em := NewExchangeManager()

	// Non-initiator message with no matching exchange should error.
	msg := &Message{
		Header: MessageHeader{SessionID: 1},
		Protocol: ProtocolHeader{
			ExchangeFlags: 0, // not initiator
			ExchangeID:    999,
		},
	}

	err := em.HandleMessage(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for unmatched non-initiator message")
	}
}

func TestExchangeManager_CloseExchange(t *testing.T) {
	em := NewExchangeManager()
	session := &Session{ID: 1}

	e, err := em.NewExchange(context.Background(), session)
	if err != nil {
		t.Fatalf("NewExchange: %v", err)
	}

	em.CloseExchange(e)

	if em.Count() != 0 {
		t.Errorf("Count after close: got %d, want 0", em.Count())
	}

	// Receive on closed exchange should fail.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = e.Receive(ctx)
	if err == nil {
		t.Fatal("expected error receiving on closed exchange")
	}
}

func TestExchange_CloseIdempotent(t *testing.T) {
	e := &Exchange{
		incoming: make(chan *Message, 1),
	}
	e.Close()
	e.Close() // should not panic
}

func TestExchange_ReceiveCancelledContext(t *testing.T) {
	e := &Exchange{
		incoming: make(chan *Message, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.Receive(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestExchangeManager_ConcurrentNewExchange(t *testing.T) {
	em := NewExchangeManager()
	session := &Session{ID: 1}
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := em.NewExchange(context.Background(), session)
			if err != nil {
				t.Errorf("NewExchange: %v", err)
			}
		}()
	}

	wg.Wait()

	if em.Count() != goroutines {
		t.Errorf("Count: got %d, want %d", em.Count(), goroutines)
	}
}
