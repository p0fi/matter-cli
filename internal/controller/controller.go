// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package controller ties together transport, protocol framing, session
// management, secure channel establishment, and interaction model into a
// single Controller that can commission and communicate with Matter devices.
package controller

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/secure"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/p0fi/matter-cli/internal/transport"
)

// Config holds configuration for creating a Controller.
type Config struct {
	// Store is the persistent store for fabrics and nodes.
	Store store.Store
	// FabricID is the fabric identity to use. If zero, a default (1) is used.
	FabricID uint64
	// BindAddr is the local UDP address to bind to. Defaults to ":0".
	BindAddr string
}

// Controller is a Matter controller that can establish PASE/CASE sessions and
// communicate with devices over the interaction model.
type Controller struct {
	conn      transport.Conn
	codec     *protocol.Codec
	sessions  *protocol.SessionTable
	exchanges *protocol.ExchangeManager
	store     store.Store
	fabricID  uint64

	// sourceNodeID is the ephemeral node ID included in unsecured messages.
	// The Matter spec requires Source Node ID to be present in all unsecured
	// messages (section 4.6.2).
	sourceNodeID uint64

	// Fabric identity (populated by initFabric).
	fabric *fabricIdentity

	// peerAddr is the current peer address for outgoing messages.
	mu       sync.Mutex
	peerAddr net.Addr

	cancel context.CancelFunc
	done   chan struct{}
}

// New creates a new Controller with the given configuration.
func New(cfg Config) (*Controller, error) {
	if cfg.FabricID == 0 {
		cfg.FabricID = 1
	}
	if cfg.BindAddr == "" {
		cfg.BindAddr = ":0"
	}

	conn, err := transport.NewUDPConn(cfg.BindAddr)
	if err != nil {
		return nil, fmt.Errorf("controller: creating UDP connection: %w", err)
	}

	c := &Controller{
		conn:         conn,
		codec:        protocol.NewCodec(),
		sessions:     protocol.NewSessionTable(),
		exchanges:    protocol.NewExchangeManager(),
		store:        cfg.Store,
		fabricID:     cfg.FabricID,
		sourceNodeID: randomNodeID(),
		done:         make(chan struct{}),
	}

	// Wire up the exchange manager's default send function.
	c.exchanges.DefaultSendFunc = c.sendMessage

	// Initialize or load fabric identity.
	if err := c.initFabric(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("controller: initializing fabric: %w", err)
	}

	return c, nil
}

// NewWithConn creates a Controller using an externally provided transport.Conn.
// This is primarily useful for testing with mock transports.
func NewWithConn(cfg Config, conn transport.Conn) (*Controller, error) {
	if cfg.FabricID == 0 {
		cfg.FabricID = 1
	}

	c := &Controller{
		conn:         conn,
		codec:        protocol.NewCodec(),
		sessions:     protocol.NewSessionTable(),
		exchanges:    protocol.NewExchangeManager(),
		store:        cfg.Store,
		fabricID:     cfg.FabricID,
		sourceNodeID: randomNodeID(),
		done:         make(chan struct{}),
	}

	c.exchanges.DefaultSendFunc = c.sendMessage

	if cfg.Store != nil {
		if err := c.initFabric(); err != nil {
			return nil, fmt.Errorf("controller: initializing fabric: %w", err)
		}
	}

	return c, nil
}

// randomNodeID generates a random 64-bit node ID for use as the source node
// ID in unsecured messages.
func randomNodeID() uint64 {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	// Ensure non-zero.
	id := binary.BigEndian.Uint64(buf[:])
	if id == 0 {
		id = 1
	}
	return id
}

// sendMessage encodes a Message using the codec and sends it over the
// current transport to the peer.
func (c *Controller) sendMessage(ctx context.Context, msg *protocol.Message) error {
	c.mu.Lock()
	addr := c.peerAddr
	c.mu.Unlock()

	if addr == nil {
		return fmt.Errorf("controller: no peer address set")
	}

	// Matter spec §4.11.8.3: MRP SHALL NOT be used on BLE transport.
	// BTP provides its own reliability at the transport layer, so the
	// exchange-level reliable flag (R) must be cleared for BLE sessions.
	// The CHIP SDK enforces this via AllowsMRP() → false for BLE.
	if _, isBLE := c.conn.(*transport.BLEConn); isBLE {
		msg.Protocol.ExchangeFlags &^= protocol.ExFlagReliable
	}

	// Look up session for encryption.
	session := c.sessions.GetSession(msg.Header.SessionID)
	if session == nil {
		session = c.sessions.UnsecuredSession()
	}

	// For unsecured messages, the Matter spec (section 4.6.2) requires the
	// Source Node ID to be present.
	if session.Type == protocol.SessionUnsecured {
		msg.Header.HasSourceNodeID = true
		msg.Header.SourceNodeID = c.sourceNodeID
	} else {
		// For secure sessions, the wire Session ID must be the peer's session
		// ID so the receiver can look up the session on their end.
		msg.Header.SessionID = session.PeerSessionID
	}

	// Stamp the message counter.
	msg.Header.MessageCounter = session.NextMessageCounter()

	data, err := c.codec.Encode(msg, session)
	if err != nil {
		return fmt.Errorf("controller: encoding message: %w", err)
	}

	slog.Debug("controller: sending", "bytes", len(data), "to", addr, "opcode", fmt.Sprintf("0x%02x", msg.Protocol.ProtocolOpcode), "exchangeID", msg.Protocol.ExchangeID, "hex", hex.EncodeToString(data))

	return c.conn.Send(ctx, data, addr)
}

// runMessagePump reads messages from the transport and dispatches them to the
// exchange manager. It runs until the context is cancelled.
func (c *Controller) runMessagePump(ctx context.Context) {
	defer close(c.done)

	for {
		data, addr, err := c.conn.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, transport.ErrConnClosed) {
				slog.Debug("controller: connection closed, stopping message pump")
				// Close all active exchanges so any goroutine blocked
				// in Exchange.Receive unblocks immediately instead of
				// waiting for a context timeout (e.g. 30 s).
				c.exchanges.CloseAll()
				return
			}
			slog.Warn("controller: receive error", "err", err)
			continue
		}

		slog.Debug("controller: received", "bytes", len(data), "from", addr, "hex", hex.EncodeToString(data))

		// Update peer address from the last received message.
		c.mu.Lock()
		c.peerAddr = addr
		c.mu.Unlock()

		// Decode the message header to determine the session.
		header, _, headerErr := protocol.DecodeMessageHeader(data)
		if headerErr != nil {
			slog.Warn("controller: decode header error", "err", headerErr, "hex", hex.EncodeToString(data))
			continue
		}

		slog.Debug("controller: header decoded", "sessionID", header.SessionID, "counter", header.MessageCounter, "flags", fmt.Sprintf("0x%02x", header.Flags), "secFlags", fmt.Sprintf("0x%02x", header.SecurityFlags))

		// Look up session for decryption.
		session := c.sessions.GetSession(header.SessionID)
		if session == nil {
			session = c.sessions.UnsecuredSession()
		}

		msg, decodeErr := c.codec.Decode(data, session)
		if decodeErr != nil {
			slog.Warn("controller: decode message error", "err", decodeErr, "sessionID", header.SessionID)
			continue
		}

		slog.Debug("controller: message decoded", "exchangeID", msg.Protocol.ExchangeID, "opcode", fmt.Sprintf("0x%02x", msg.Protocol.ProtocolOpcode), "protocolID", fmt.Sprintf("0x%04x", msg.Protocol.ProtocolID), "exFlags", fmt.Sprintf("0x%02x", msg.Protocol.ExchangeFlags))

		// If the message requests reliable delivery (R flag), send a standalone
		// MRP acknowledgment so the peer knows we received it.
		// On BLE transport, MRP is not used so we skip MRP ACKs entirely.
		if msg.Protocol.NeedsACK() {
			if _, isBLE := c.conn.(*transport.BLEConn); !isBLE {
				c.sendMRPAck(ctx, msg)
			}
		}

		// Skip standalone MRP ACKs — they carry no application payload and
		// should not be delivered to the exchange handler.
		if msg.Protocol.ProtocolOpcode == 0x10 && msg.Protocol.HasAckCounter {
			slog.Debug("controller: received standalone MRP ack", "ackCounter", msg.Protocol.AckMessageCounter)
			continue
		}

		if err := c.exchanges.HandleMessage(ctx, msg); err != nil {
			slog.Warn("controller: handle message error", "err", err, "exchangeID", msg.Protocol.ExchangeID, "opcode", fmt.Sprintf("0x%02x", msg.Protocol.ProtocolOpcode))
		}
	}
}

// sendMRPAck sends a standalone MRP acknowledgment for the given message.
func (c *Controller) sendMRPAck(ctx context.Context, msg *protocol.Message) {
	ack := &protocol.Message{
		Header: protocol.MessageHeader{
			SessionID: msg.Header.SessionID,
		},
		Protocol: protocol.ProtocolHeader{
			ExchangeFlags:    protocol.ExFlagACK,
			ProtocolOpcode:   0x10,   // MRP Standalone Ack
			ProtocolID:       0x0000, // Secure Channel protocol
			ExchangeID:       msg.Protocol.ExchangeID,
			HasAckCounter:    true,
			AckMessageCounter: msg.Header.MessageCounter,
		},
	}
	// Mirror the initiator flag from the received message — the ACK is sent
	// from the same role as the receiver (i.e., if we are NOT the initiator,
	// we don't set the flag).
	if !msg.Protocol.IsInitiator() {
		// We are the initiator, set the flag on our ACK.
		ack.Protocol.ExchangeFlags |= protocol.ExFlagInitiator
	}

	if err := c.sendMessage(ctx, ack); err != nil {
		slog.Debug("controller: failed to send MRP ack", "err", err)
	}
}

// startMessagePump starts the message pump in a background goroutine and
// returns a cancel function. The pump can be stopped by calling the cancel
// function or by closing the controller.
func (c *Controller) startMessagePump() context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.done = make(chan struct{})
	go c.runMessagePump(ctx)
	return cancel
}

// ConnectPASE establishes a PASE session with a device at the given address
// using the provided passcode. Returns the secured session.
func (c *Controller) ConnectPASE(ctx context.Context, addr string, passcode uint32) (*protocol.Session, error) {
	// Resolve the peer address.
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("controller: resolving address %q: %w", addr, err)
	}

	c.mu.Lock()
	c.peerAddr = udpAddr
	c.mu.Unlock()

	return c.connectPASEWithAddr(ctx, passcode)
}

// connectPASEWithAddr performs the PASE handshake assuming the peer address is already set.
func (c *Controller) connectPASEWithAddr(ctx context.Context, passcode uint32) (*protocol.Session, error) {
	// Apply a timeout if the caller didn't set one.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// Start the message pump.
	if c.cancel == nil {
		c.startMessagePump()
	}

	// Pre-allocate the secure session to get a non-zero proposed session ID.
	// The Matter spec requires the initiator to propose a session ID for the
	// new secure session in the PBKDFParamRequest message.
	session, err := c.sessions.CreateSession(protocol.SessionPASE)
	if err != nil {
		return nil, fmt.Errorf("controller: creating PASE session: %w", err)
	}

	// Create an unsecured exchange for the PASE handshake.
	unsecured := c.sessions.UnsecuredSession()
	exchange, err := c.exchanges.NewExchange(ctx, unsecured)
	if err != nil {
		c.sessions.RemoveSession(session.ID)
		return nil, fmt.Errorf("controller: creating exchange: %w", err)
	}
	defer c.exchanges.CloseExchange(exchange)

	// Run the PASE handshake with the pre-allocated session ID.
	keys, peerSessionID, err := secure.EstablishPASE(ctx, exchange, passcode, session.ID)
	if err != nil {
		c.sessions.RemoveSession(session.ID)
		return nil, fmt.Errorf("controller: PASE handshake: %w", err)
	}

	// Populate the session with the derived keys.
	session.PeerSessionID = peerSessionID
	session.EncryptKey = keys.I2RKey
	session.DecryptKey = keys.R2IKey
	session.AttestationChallenge = keys.AttestationChallenge

	return session, nil
}

// ConnectCASE establishes a CASE session with a device at the given address.
// Returns the secured session.
func (c *Controller) ConnectCASE(ctx context.Context, addr string, nodeID uint64) (*protocol.Session, error) {
	if c.fabric == nil {
		return nil, fmt.Errorf("controller: no fabric identity configured")
	}

	// Apply a timeout if the caller didn't set one.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("controller: resolving address %q: %w", addr, err)
	}

	c.mu.Lock()
	c.peerAddr = udpAddr
	c.mu.Unlock()

	// Start pump if not already running.
	if c.cancel == nil {
		c.startMessagePump()
	}

	// Pre-allocate the secure session to get a proposed session ID.
	session, err := c.sessions.CreateSession(protocol.SessionCASE)
	if err != nil {
		return nil, fmt.Errorf("controller: creating CASE session: %w", err)
	}

	unsecured := c.sessions.UnsecuredSession()
	exchange, err := c.exchanges.NewExchange(ctx, unsecured)
	if err != nil {
		c.sessions.RemoveSession(session.ID)
		return nil, fmt.Errorf("controller: creating exchange: %w", err)
	}
	defer c.exchanges.CloseExchange(exchange)

	cfg := secure.CASEInitiatorConfig{
		SessionID:  session.ID,
		NodeKey:    c.fabric.nodeKey,
		NOC:        c.fabric.nocTLV,
		ICAC:       c.fabric.icacTLV,
		IPK:        c.fabric.operationalIPK,
		RootPubKey: c.fabric.rootPubKey,
		FabricID:   c.fabricID,
		PeerNodeID: nodeID,
	}

	keys, peerSessionID, err := secure.EstablishCASE(ctx, exchange, cfg)
	if err != nil {
		c.sessions.RemoveSession(session.ID)
		return nil, fmt.Errorf("controller: CASE handshake: %w", err)
	}

	session.PeerSessionID = peerSessionID
	session.PeerNodeID = nodeID
	session.LocalNodeID = c.fabricID // controller's operational node ID
	session.EncryptKey = keys.I2RKey
	session.DecryptKey = keys.R2IKey
	session.AttestationChallenge = keys.AttestationChallenge

	return session, nil
}

// Sessions returns the controller's session table.
func (c *Controller) Sessions() *protocol.SessionTable {
	return c.sessions
}

// Exchanges returns the controller's exchange manager.
func (c *Controller) Exchanges() *protocol.ExchangeManager {
	return c.exchanges
}

// Close shuts down the controller, stopping the message pump and closing the
// transport connection.
func (c *Controller) Close() error {
	if c.cancel != nil {
		c.cancel()
		<-c.done
	}
	return c.conn.Close()
}
