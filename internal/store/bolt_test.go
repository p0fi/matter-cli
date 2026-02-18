// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"path/filepath"
	"testing"
)

func TestBoltStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := NewBoltStore(path)
	if err != nil {
		t.Fatalf("NewBoltStore: %v", err)
	}
	defer s.Close()
	storeTestSuite(t, s)
}

func TestDefaultDBPath(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		p, err := DefaultDBPath()
		if err != nil {
			t.Fatalf("DefaultDBPath: %v", err)
		}
		if p == "" {
			t.Error("DefaultDBPath returned empty string")
		}
	})

	t.Run("XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
		p, err := DefaultDBPath()
		if err != nil {
			t.Fatalf("DefaultDBPath: %v", err)
		}
		want := "/tmp/xdg-test/matter-cli/matter.db"
		if p != want {
			t.Errorf("DefaultDBPath = %q, want %q", p, want)
		}
	})
}
