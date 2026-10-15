// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/p0fi/matter-cli/internal/protocol"
)

// fakeDialer records CASE attempts and answers from a per-address table.
type fakeDialer struct {
	results   map[string]error
	calls     []string
	deadlines []time.Duration // remaining budget each attempt was given
}

func (f *fakeDialer) dial(ctx context.Context, addr string, _ uint64) (*protocol.Session, error) {
	f.calls = append(f.calls, addr)
	if dl, ok := ctx.Deadline(); ok {
		f.deadlines = append(f.deadlines, time.Until(dl))
	} else {
		f.deadlines = append(f.deadlines, 0)
	}
	if err := f.results[addr]; err != nil {
		return nil, err
	}
	return &protocol.Session{}, nil
}

func TestConnectWithRediscovery(t *testing.T) {
	const (
		stale = "192.168.1.10:5540"
		fresh = "10.0.0.23:5540"
	)
	errCASE := errors.New("receiving Sigma2: context deadline exceeded")

	t.Run("stored address works without rediscovery", func(t *testing.T) {
		d := &fakeDialer{}
		resolve := func(context.Context, uint64, time.Duration) (string, error) {
			t.Fatal("resolve must not be called when the stored address works")
			return "", nil
		}
		_, addr, err := connectWithRediscovery(context.Background(), d.dial, resolve, stale, 25, nil)
		if err != nil || addr != stale {
			t.Fatalf("got addr=%q err=%v, want %q, nil", addr, err, stale)
		}
	})

	t.Run("stale address is rediscovered, persisted, and retried", func(t *testing.T) {
		d := &fakeDialer{results: map[string]error{stale: errCASE}}
		resolve := func(context.Context, uint64, time.Duration) (string, error) { return fresh, nil }
		var persisted string
		_, addr, err := connectWithRediscovery(context.Background(), d.dial, resolve, stale, 25,
			func(a string) { persisted = a })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if addr != fresh || persisted != fresh {
			t.Errorf("addr=%q persisted=%q, want both %q", addr, persisted, fresh)
		}
		if want := []string{stale, fresh}; strings.Join(d.calls, ",") != strings.Join(want, ",") {
			t.Errorf("dial calls = %v, want %v", d.calls, want)
		}
	})

	t.Run("rediscovery failure is reported alongside the CASE error", func(t *testing.T) {
		d := &fakeDialer{results: map[string]error{stale: errCASE}}
		errResolve := errors.New("node 0000000000000019 not found")
		resolve := func(context.Context, uint64, time.Duration) (string, error) { return "", errResolve }
		_, _, err := connectWithRediscovery(context.Background(), d.dial, resolve, stale, 25, nil)
		if !errors.Is(err, errCASE) || !errors.Is(err, errResolve) {
			t.Fatalf("err = %v, want it to wrap both the CASE and the rediscovery error", err)
		}
		if !strings.Contains(err.Error(), "operational rediscovery failed") {
			t.Errorf("err = %q, want it to mention the rediscovery attempt", err)
		}
	})

	t.Run("retry failure names the rediscovered address", func(t *testing.T) {
		d := &fakeDialer{results: map[string]error{stale: errCASE, fresh: errCASE}}
		resolve := func(context.Context, uint64, time.Duration) (string, error) { return fresh, nil }
		_, _, err := connectWithRediscovery(context.Background(), d.dial, resolve, stale, 25, nil)
		if !errors.Is(err, errCASE) || !strings.Contains(err.Error(), fresh) {
			t.Fatalf("err = %v, want it to wrap the CASE error and name %s", err, fresh)
		}
	})

	t.Run("cancelled caller skips rediscovery", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		d := &fakeDialer{results: map[string]error{stale: errCASE}}
		dial := func(ctx context.Context, addr string, id uint64) (*protocol.Session, error) {
			cancel()
			return d.dial(ctx, addr, id)
		}
		resolve := func(context.Context, uint64, time.Duration) (string, error) {
			t.Fatal("resolve must not run after the caller gave up")
			return "", nil
		}
		if _, _, err := connectWithRediscovery(ctx, dial, resolve, stale, 25, nil); !errors.Is(err, errCASE) {
			t.Fatalf("err = %v, want the CASE error", err)
		}
	})

	// A stale address never answers. If the first attempt could use the
	// caller's whole budget (e.g. the 30 s cluster read timeout), rediscovery
	// and the retry would start with nothing left.
	t.Run("first attempt leaves budget for rediscovery and retry", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d := &fakeDialer{results: map[string]error{stale: errCASE}}
		var resolveBudget time.Duration
		resolve := func(ctx context.Context, _ uint64, timeout time.Duration) (string, error) {
			if ctx.Err() != nil {
				t.Fatal("rediscovery started with an expired context")
			}
			resolveBudget = timeout
			return fresh, nil
		}
		if _, _, err := connectWithRediscovery(ctx, d.dial, resolve, stale, 25, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.deadlines[0] > storedAddrCASETimeout {
			t.Errorf("first attempt budget = %s, want at most %s", d.deadlines[0], storedAddrCASETimeout)
		}
		if resolveBudget != rediscoveryTimeout {
			t.Errorf("rediscovery budget = %s, want %s", resolveBudget, rediscoveryTimeout)
		}
		if d.deadlines[1] <= storedAddrCASETimeout {
			t.Errorf("retry budget = %s, want the caller's remaining budget", d.deadlines[1])
		}
	})
}

func TestPickBestIP(t *testing.T) {
	v4 := net.ParseIP("192.168.1.5")
	global := net.ParseIP("fd00::5")
	linkLocal := net.ParseIP("fe80::5")

	tests := []struct {
		name string
		ips  []net.IP
		want net.IP
	}{
		{"empty", nil, nil},
		{"global v6 beats v4", []net.IP{v4, global}, global},
		{"v4 beats link-local", []net.IP{linkLocal, v4}, v4},
		{"link-local as last resort", []net.IP{linkLocal}, linkLocal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pickBestIP(tt.ips); !got.Equal(tt.want) {
				t.Errorf("pickBestIP = %v, want %v", got, tt.want)
			}
		})
	}
}
