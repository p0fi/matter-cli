// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package discovery

import (
	"context"
	"fmt"

	"github.com/grandcat/zeroconf"
)

// zeroconfResolver is the production implementation using grandcat/zeroconf.
// This works on Linux where Go can bind UDP port 5353 and receive multicast
// traffic directly. On macOS, mDNSResponder monopolizes port 5353 so this
// resolver receives no traffic — see mdns_dnssd_darwin.go for the macOS path.
type zeroconfResolver struct{}

func (z *zeroconfResolver) Browse(ctx context.Context, service, domain string, entries chan<- *zeroconf.ServiceEntry) error {
	r, err := zeroconf.NewResolver(nil)
	if err != nil {
		return fmt.Errorf("creating mDNS resolver: %w", err)
	}
	return r.Browse(ctx, service, domain, entries)
}

// NewMDNSBrowser returns a new MDNSBrowser that uses grandcat/zeroconf for
// mDNS service discovery. This works on Linux where Go can bind UDP port 5353
// and receive multicast traffic directly.
func NewMDNSBrowser() *MDNSBrowser {
	return &MDNSBrowser{
		resolver: &zeroconfResolver{},
	}
}
