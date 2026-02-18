// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"sync"
	"testing"
)

func TestSessionTable_CreateAndGet(t *testing.T) {
	st := NewSessionTable()

	s, err := st.CreateSession(SessionPASE)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.ID == 0 {
		t.Error("expected non-zero session ID")
	}
	if s.Type != SessionPASE {
		t.Errorf("Type: got %v, want %v", s.Type, SessionPASE)
	}

	got := st.GetSession(s.ID)
	if got != s {
		t.Error("GetSession returned different session")
	}

	if st.GetSession(9999) != nil {
		t.Error("expected nil for non-existent session")
	}
}

func TestSessionTable_UnsecuredSession(t *testing.T) {
	st := NewSessionTable()

	s1 := st.UnsecuredSession()
	s2 := st.UnsecuredSession()
	if s1 != s2 {
		t.Error("UnsecuredSession should return the same instance")
	}
	if s1.ID != 0 {
		t.Errorf("unsecured session ID: got %d, want 0", s1.ID)
	}
	if s1.Type != SessionUnsecured {
		t.Errorf("unsecured session type: got %v, want %v", s1.Type, SessionUnsecured)
	}
}

func TestSessionTable_Remove(t *testing.T) {
	st := NewSessionTable()

	s, err := st.CreateSession(SessionCASE)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	st.RemoveSession(s.ID)
	if st.GetSession(s.ID) != nil {
		t.Error("session should be removed")
	}
}

func TestSessionTable_Count(t *testing.T) {
	st := NewSessionTable()

	if st.Count() != 0 {
		t.Errorf("Count: got %d, want 0", st.Count())
	}

	st.UnsecuredSession()
	if st.Count() != 1 {
		t.Errorf("Count: got %d, want 1", st.Count())
	}

	_, _ = st.CreateSession(SessionPASE)
	if st.Count() != 2 {
		t.Errorf("Count: got %d, want 2", st.Count())
	}
}

func TestSession_MessageCounter(t *testing.T) {
	s := &Session{}
	c0 := s.NextMessageCounter()
	c1 := s.NextMessageCounter()
	c2 := s.NextMessageCounter()

	if c0 != 0 {
		t.Errorf("first counter: got %d, want 0", c0)
	}
	if c1 != 1 {
		t.Errorf("second counter: got %d, want 1", c1)
	}
	if c2 != 2 {
		t.Errorf("third counter: got %d, want 2", c2)
	}
}

func TestSession_PeerCounterValidation(t *testing.T) {
	tests := []struct {
		name     string
		counters []uint32
		wantErr  []bool
	}{
		{
			name:     "sequential",
			counters: []uint32{1, 2, 3, 4, 5},
			wantErr:  []bool{false, false, false, false, false},
		},
		{
			name:     "duplicate",
			counters: []uint32{1, 2, 2},
			wantErr:  []bool{false, false, true},
		},
		{
			name:     "out of order within window",
			counters: []uint32{1, 3, 2, 4},
			wantErr:  []bool{false, false, false, false},
		},
		{
			name:     "too old",
			counters: []uint32{100, 50},
			wantErr:  []bool{false, true},
		},
		{
			name:     "at window boundary",
			counters: []uint32{32, 0},
			wantErr:  []bool{false, true},
		},
		{
			name:     "just inside window",
			counters: []uint32{31, 0},
			wantErr:  []bool{false, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Session{}
			for i, c := range tt.counters {
				err := s.ValidatePeerCounter(c)
				if (err != nil) != tt.wantErr[i] {
					t.Errorf("counter %d (value=%d): err=%v, wantErr=%v", i, c, err, tt.wantErr[i])
				}
			}
		})
	}
}

func TestSessionTable_ConcurrentAccess(t *testing.T) {
	st := NewSessionTable()
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			s, err := st.CreateSession(SessionCASE)
			if err != nil {
				t.Errorf("CreateSession: %v", err)
				return
			}

			got := st.GetSession(s.ID)
			if got == nil {
				t.Errorf("GetSession returned nil for ID %d", s.ID)
			}

			_ = st.Count()
		}()
	}

	wg.Wait()

	// Verify we created the right number of sessions.
	if st.Count() != goroutines {
		t.Errorf("Count: got %d, want %d", st.Count(), goroutines)
	}
}

func TestSession_PeerCounterConcurrent(t *testing.T) {
	s := &Session{}
	const goroutines = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(counter uint32) {
			defer wg.Done()
			_ = s.ValidatePeerCounter(counter)
		}(uint32(i))
	}

	wg.Wait()
}

func TestSessionType_String(t *testing.T) {
	tests := []struct {
		st   SessionType
		want string
	}{
		{SessionUnsecured, "Unsecured"},
		{SessionPASE, "PASE"},
		{SessionCASE, "CASE"},
		{SessionType(99), "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.st.String(); got != tt.want {
			t.Errorf("SessionType(%d).String(): got %q, want %q", tt.st, got, tt.want)
		}
	}
}
