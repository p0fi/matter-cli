// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package store

import "testing"

func TestMemoryStore(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	storeTestSuite(t, s)
}
