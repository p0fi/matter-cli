// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
)

// SessionType distinguishes between the different Matter session types.
type SessionType int

const (
	// SessionUnsecured represents an unauthenticated session (session ID 0).
	SessionUnsecured SessionType = iota
	// SessionPASE represents a session established via PASE (Passcode-Authenticated Session Establishment).
	SessionPASE
	// SessionCASE represents a session established via CASE (Certificate-Authenticated Session Establishment).
	SessionCASE
)

// String returns a human-readable name for the session type.
func (st SessionType) String() string {
	switch st {
	case SessionUnsecured:
		return "Unsecured"
	case SessionPASE:
		return "PASE"
	case SessionCASE:
		return "CASE"
	default:
		return "Unknown"
	}
}

// Session represents a Matter communication session with its security context.
type Session struct {
	// ID is the local session identifier.
	ID uint16
	// PeerSessionID is the peer's session identifier.
	PeerSessionID uint16
	// Type is the session type (Unsecured, PASE, CASE).
	Type SessionType
	// LocalNodeID is the local (our) operational node ID for this session.
	// Used in nonce construction when sending messages.
	LocalNodeID uint64
	// PeerNodeID is the peer's node ID.
	// Used in nonce construction when receiving messages (if source node ID not in header).
	PeerNodeID uint64
	// EncryptKey is the session encryption key (16 bytes for AES-128-CCM).
	EncryptKey []byte
	// DecryptKey is the session decryption key (16 bytes for AES-128-CCM).
	DecryptKey []byte
	// AttestationChallenge is the attestation challenge for this session.
	AttestationChallenge []byte

	// localCounter is the local outgoing message counter.
	localCounter atomic.Uint32
	// peerCounter tracks the peer's message counter for replay protection.
	peerCounter peerMessageCounter
}

// NextMessageCounter returns the next outgoing message counter and increments it.
func (s *Session) NextMessageCounter() uint32 {
	return s.localCounter.Add(1) - 1
}

// ValidatePeerCounter checks whether a received message counter is valid
// (not a replay). It updates the tracking state if valid. Thread-safe.
func (s *Session) ValidatePeerCounter(counter uint32) error {
	return s.peerCounter.validate(counter)
}

// peerMessageCounter tracks peer message counters for replay protection.
// It uses a simple high-water mark with a small bitmap for out-of-order messages.
type peerMessageCounter struct {
	mu        sync.Mutex
	maxSeen   uint32
	initiated bool
	// bitmap tracks receipt of counters in [maxSeen-31, maxSeen].
	// Bit i corresponds to counter (maxSeen - i).
	bitmap uint32
}

// windowSize is the number of recent counters tracked for out-of-order detection.
const windowSize = 32

func (pc *peerMessageCounter) validate(counter uint32) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if !pc.initiated {
		pc.maxSeen = counter
		pc.initiated = true
		pc.bitmap = 1 // bit 0 = maxSeen itself
		return nil
	}

	if counter > pc.maxSeen {
		// Advance the window.
		diff := counter - pc.maxSeen
		if diff >= windowSize {
			pc.bitmap = 0
		} else {
			pc.bitmap <<= diff
		}
		pc.bitmap |= 1 // mark new counter as seen
		pc.maxSeen = counter
		return nil
	}

	// Counter is within or below the window.
	diff := pc.maxSeen - counter
	if diff >= windowSize {
		return fmt.Errorf("protocol: message counter %d too old (max seen %d)", counter, pc.maxSeen)
	}

	bit := uint32(1) << diff
	if pc.bitmap&bit != 0 {
		return fmt.Errorf("protocol: duplicate message counter %d", counter)
	}
	pc.bitmap |= bit
	return nil
}

// SessionTable manages active Matter sessions. It is safe for concurrent use.
type SessionTable struct {
	mu       sync.RWMutex
	sessions map[uint16]*Session
	nextID   uint16
}

// NewSessionTable creates a new SessionTable. Session IDs start from 1
// (ID 0 is reserved for unsecured sessions).
func NewSessionTable() *SessionTable {
	return &SessionTable{
		sessions: make(map[uint16]*Session),
		nextID:   1,
	}
}

// UnsecuredSession returns the shared unsecured session (ID 0).
// The message counter is initialized to a random value as required by the Matter spec.
func (st *SessionTable) UnsecuredSession() *Session {
	st.mu.RLock()
	s, ok := st.sessions[0]
	st.mu.RUnlock()
	if ok {
		return s
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	// Double-check after acquiring write lock.
	if s, ok := st.sessions[0]; ok {
		return s
	}
	s = &Session{
		ID:   0,
		Type: SessionUnsecured,
	}
	// Initialize the message counter to a random value (Matter spec 4.5.1.1).
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err == nil {
		s.localCounter.Store(binary.LittleEndian.Uint32(buf[:]))
	}
	st.sessions[0] = s
	return s
}

// CreateSession allocates a new session with the given type and returns it.
func (st *SessionTable) CreateSession(sessionType SessionType) (*Session, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Find next available ID (skip 0 which is reserved for unsecured).
	startID := st.nextID
	for {
		id := st.nextID
		st.nextID++
		if st.nextID == 0 {
			st.nextID = 1 // wrap around, skip 0
		}
		if _, exists := st.sessions[id]; !exists {
			s := &Session{
				ID:   id,
				Type: sessionType,
			}
			// Initialize the message counter to a random value per Matter spec 4.5.1.1.
			var buf [4]byte
			if _, err := rand.Read(buf[:]); err == nil {
				s.localCounter.Store(binary.LittleEndian.Uint32(buf[:]))
			}
			st.sessions[id] = s
			return s, nil
		}
		if st.nextID == startID {
			return nil, fmt.Errorf("protocol: session table full")
		}
	}
}

// GetSession looks up a session by its local ID. Returns nil if not found.
func (st *SessionTable) GetSession(id uint16) *Session {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.sessions[id]
}

// RemoveSession removes a session by its local ID.
func (st *SessionTable) RemoveSession(id uint16) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.sessions, id)
}

// Count returns the number of active sessions (including the unsecured session if present).
func (st *SessionTable) Count() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.sessions)
}
