// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/grandcat/zeroconf"
)

// Browser defines the interface for discovering Matter devices on the network.
// Implementations must be safe for concurrent use.
type Browser interface {
	// DiscoverCommissionable browses for devices advertising _matterc._udp
	// and returns all devices found within the given timeout.
	DiscoverCommissionable(ctx context.Context, timeout time.Duration) ([]*Device, error)

	// DiscoverOperational browses for devices advertising _matter._tcp
	// and returns all devices found within the given timeout.
	DiscoverOperational(ctx context.Context, timeout time.Duration) ([]*Device, error)
}

// resolver abstracts the mDNS resolver for testability.
type resolver interface {
	Browse(ctx context.Context, service, domain string, entries chan<- *zeroconf.ServiceEntry) error
}

// zeroconfResolver is the production implementation using grandcat/zeroconf.
type zeroconfResolver struct{}

func (z *zeroconfResolver) Browse(ctx context.Context, service, domain string, entries chan<- *zeroconf.ServiceEntry) error {
	r, err := zeroconf.NewResolver(nil)
	if err != nil {
		return fmt.Errorf("creating mDNS resolver: %w", err)
	}
	return r.Browse(ctx, service, domain, entries)
}

// MDNSBrowser discovers Matter devices using mDNS (multicast DNS).
type MDNSBrowser struct {
	resolver resolver
}

// NewMDNSBrowser returns a new MDNSBrowser that uses the system's mDNS capabilities.
func NewMDNSBrowser() *MDNSBrowser {
	return &MDNSBrowser{
		resolver: &zeroconfResolver{},
	}
}

// newMDNSBrowserWithResolver creates a browser with a custom resolver (for testing).
func newMDNSBrowserWithResolver(r resolver) *MDNSBrowser {
	return &MDNSBrowser{resolver: r}
}

// DiscoverCommissionable browses for commissionable Matter devices advertising
// the _matterc._udp service and returns all devices found within the given timeout.
func (b *MDNSBrowser) DiscoverCommissionable(ctx context.Context, timeout time.Duration) ([]*Device, error) {
	return b.browse(ctx, timeout, ServiceCommissionable)
}

// DiscoverOperational browses for operational Matter devices advertising
// the _matter._tcp service and returns all devices found within the given timeout.
func (b *MDNSBrowser) DiscoverOperational(ctx context.Context, timeout time.Duration) ([]*Device, error) {
	return b.browse(ctx, timeout, ServiceOperational)
}

// browse performs mDNS service discovery for the given service type.
func (b *MDNSBrowser) browse(ctx context.Context, timeout time.Duration, svcType ServiceType) ([]*Device, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	entries := make(chan *zeroconf.ServiceEntry)
	var devices []*Device

	done := make(chan error, 1)
	go func() {
		done <- b.resolver.Browse(ctx, string(svcType), "local.", entries)
	}()

	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				// Channel closed — browse finished.
				if err := <-done; err != nil {
					return devices, fmt.Errorf("browsing %s: %w", svcType, err)
				}
				return devices, nil
			}
			dev := entryToDevice(entry, svcType)
			devices = append(devices, dev)
		case err := <-done:
			// Browse returned; drain remaining entries.
			for entry := range entries {
				dev := entryToDevice(entry, svcType)
				devices = append(devices, dev)
			}
			if err != nil {
				return devices, fmt.Errorf("browsing %s: %w", svcType, err)
			}
			return devices, nil
		}
	}
}

// entryToDevice converts a zeroconf service entry to a Device.
func entryToDevice(entry *zeroconf.ServiceEntry, svcType ServiceType) *Device {
	dev := &Device{
		Name:        entry.Instance,
		Host:        entry.HostName,
		Port:        entry.Port,
		ServiceType: svcType,
	}

	// Collect all IP addresses (both v4 and v6).
	dev.IPs = make([]net.IP, 0, len(entry.AddrIPv4)+len(entry.AddrIPv6))
	for _, ip := range entry.AddrIPv4 {
		dev.IPs = append(dev.IPs, ip)
	}
	for _, ip := range entry.AddrIPv6 {
		dev.IPs = append(dev.IPs, ip)
	}

	dev.parseTXTRecords(entry.Text)
	return dev
}
