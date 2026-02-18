// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// Exchange represents a single Matter exchange — a sequence of messages
// between two nodes within a session.
type Exchange struct {
	// ID is the exchange identifier.
	ID uint16
	// Session is the session this exchange belongs to.
	Session *Session
	// IsInitiator indicates whether the local node initiated this exchange.
	IsInitiator bool

	// SendFunc is the function used to send messages over the transport.
	// If nil, Send is a no-op (for backward compatibility with tests).
	SendFunc func(ctx context.Context, msg *Message) error

	// incoming delivers received messages to the exchange owner.
	incoming chan *Message
	// closed tracks whether the exchange has been closed.
	closed atomic.Bool
}

// Send stamps the exchange ID and initiator flag on the message, then transmits
// it via the configured SendFunc. If SendFunc is nil, Send is a no-op (returning
// nil) for backward compatibility with tests that don't wire up a transport.
func (e *Exchange) Send(ctx context.Context, msg *Message) error {
	msg.Protocol.ExchangeID = e.ID
	if e.IsInitiator {
		msg.Protocol.ExchangeFlags |= ExFlagInitiator
	}
	msg.Header.SessionID = e.Session.ID
	if e.SendFunc == nil {
		return nil
	}
	return e.SendFunc(ctx, msg)
}

// Receive waits for the next message in this exchange, or returns an error
// if the context is cancelled or the exchange is closed.
func (e *Exchange) Receive(ctx context.Context) (*Message, error) {
	select {
	case msg, ok := <-e.incoming:
		if !ok {
			return nil, fmt.Errorf("protocol: exchange %d closed", e.ID)
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close closes the exchange and its incoming message channel.
func (e *Exchange) Close() {
	if e.closed.CompareAndSwap(false, true) {
		close(e.incoming)
	}
}

// ExchangeManager tracks active exchanges and routes incoming messages
// to the correct exchange. It is safe for concurrent use.
type ExchangeManager struct {
	mu        sync.RWMutex
	exchanges map[exchangeKey]*Exchange
	nextID    uint16

	// DefaultSendFunc is assigned to every new exchange created by NewExchange.
	// It provides the transport-level send capability.
	DefaultSendFunc func(ctx context.Context, msg *Message) error

	// OnUnhandled is called when a message arrives for an unknown exchange
	// and the sender is the initiator (i.e., this is a new incoming exchange).
	// If nil, such messages are silently dropped.
	OnUnhandled func(ctx context.Context, exchange *Exchange, msg *Message) error
}

// exchangeKey uniquely identifies an exchange within a session.
type exchangeKey struct {
	sessionID  uint16
	exchangeID uint16
	// isLocal is true if we originated this exchange.
	isLocal bool
}

// NewExchangeManager creates a new ExchangeManager.
func NewExchangeManager() *ExchangeManager {
	return &ExchangeManager{
		exchanges: make(map[exchangeKey]*Exchange),
	}
}

// NewExchange creates a new locally-initiated exchange on the given session.
func (em *ExchangeManager) NewExchange(ctx context.Context, session *Session) (*Exchange, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	em.mu.Lock()
	defer em.mu.Unlock()

	id := em.nextID
	em.nextID++

	e := &Exchange{
		ID:          id,
		Session:     session,
		IsInitiator: true,
		SendFunc:    em.DefaultSendFunc,
		incoming:    make(chan *Message, 8),
	}

	key := exchangeKey{
		sessionID:  session.ID,
		exchangeID: id,
		isLocal:    true,
	}
	em.exchanges[key] = e
	return e, nil
}

// HandleMessage routes an incoming decoded message to the appropriate exchange.
// If no matching exchange exists and the message is from an initiator, a new
// exchange is created and the OnUnhandled callback is invoked.
func (em *ExchangeManager) HandleMessage(ctx context.Context, msg *Message) error {
	isFromInitiator := msg.Protocol.IsInitiator()

	// If the remote is the initiator, the exchange is "remote" from our perspective.
	// If the remote is not the initiator, we must be the initiator, so it is "local".
	key := exchangeKey{
		sessionID:  msg.Header.SessionID,
		exchangeID: msg.Protocol.ExchangeID,
		isLocal:    !isFromInitiator,
	}

	em.mu.RLock()
	e, ok := em.exchanges[key]
	em.mu.RUnlock()

	if ok {
		if e.closed.Load() {
			return fmt.Errorf("protocol: exchange %d is closed", e.ID)
		}
		select {
		case e.incoming <- msg:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// No existing exchange. Only accept if the remote is the initiator.
	if !isFromInitiator {
		return fmt.Errorf("protocol: no exchange found for session=%d exchange=%d", msg.Header.SessionID, msg.Protocol.ExchangeID)
	}

	// Look up the session for the new exchange.
	session := &Session{ID: msg.Header.SessionID}

	e = &Exchange{
		ID:          msg.Protocol.ExchangeID,
		Session:     session,
		IsInitiator: false,
		incoming:    make(chan *Message, 8),
	}

	remoteKey := exchangeKey{
		sessionID:  msg.Header.SessionID,
		exchangeID: msg.Protocol.ExchangeID,
		isLocal:    false,
	}

	em.mu.Lock()
	em.exchanges[remoteKey] = e
	em.mu.Unlock()

	if em.OnUnhandled != nil {
		// Deliver the initial message before invoking the handler.
		select {
		case e.incoming <- msg:
		case <-ctx.Done():
			return ctx.Err()
		}
		return em.OnUnhandled(ctx, e, msg)
	}

	// No handler; drop silently.
	return nil
}

// CloseExchange removes an exchange from the manager and closes it.
func (em *ExchangeManager) CloseExchange(e *Exchange) {
	em.mu.Lock()
	defer em.mu.Unlock()

	key := exchangeKey{
		sessionID:  e.Session.ID,
		exchangeID: e.ID,
		isLocal:    e.IsInitiator,
	}
	delete(em.exchanges, key)
	e.Close()
}

// Count returns the number of active exchanges.
func (em *ExchangeManager) Count() int {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return len(em.exchanges)
}
