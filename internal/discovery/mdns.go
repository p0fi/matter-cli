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

	// WatchCommissionable performs a streaming mDNS browse for commissionable
	// devices (_matterc._udp), calling onDevice for every entry as it arrives.
	// If onDevice returns true the browse is cancelled and nil is returned
	// immediately — avoiding the full timeout wait.
	WatchCommissionable(ctx context.Context, timeout time.Duration, onDevice func(*Device) (done bool)) error
}

// resolver abstracts the mDNS resolver for testability.
type resolver interface {
	Browse(ctx context.Context, service, domain string, entries chan<- *zeroconf.ServiceEntry) error
}

// MDNSBrowser discovers Matter devices using mDNS (multicast DNS).
type MDNSBrowser struct {
	resolver resolver
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

// WatchCommissionable performs a continuous streaming mDNS browse for
// commissionable Matter devices (_matterc._udp) for up to the given timeout,
// calling onDevice for every entry as it arrives.
//
// If onDevice returns true ("found what I needed"), WatchCommissionable cancels
// the browse and returns nil immediately. If the timeout expires without
// onDevice returning true, WatchCommissionable returns nil. A non-nil error is
// only returned for transport-level failures.
func (b *MDNSBrowser) WatchCommissionable(ctx context.Context, timeout time.Duration, onDevice func(*Device) (done bool)) error {
	return b.watch(ctx, timeout, ServiceCommissionable, onDevice)
}

// DiscoverOperational browses for operational Matter devices advertising
// the _matter._tcp service and returns all devices found within the given timeout.
func (b *MDNSBrowser) DiscoverOperational(ctx context.Context, timeout time.Duration) ([]*Device, error) {
	return b.browse(ctx, timeout, ServiceOperational)
}

// WatchOperational performs a continuous streaming mDNS browse for operational
// Matter devices (_matter._tcp) for up to the given timeout, calling onDevice
// for every entry as it arrives.
//
// Unlike DiscoverOperational — which collects entries for a fixed window and
// returns them all at once — WatchOperational never misses a device that
// appears mid-browse: the mDNS multicast socket stays open for the full
// duration and every incoming announcement is delivered immediately.
//
// If onDevice returns true ("found what I needed"), WatchOperational cancels
// the browse and returns nil immediately. If the timeout expires without
// onDevice returning true, WatchOperational returns nil (not an error — an
// empty result is not a failure; the caller should handle it). A non-nil
// error is only returned for transport-level failures.
func (b *MDNSBrowser) WatchOperational(ctx context.Context, timeout time.Duration, onDevice func(*Device) (done bool)) error {
	return b.watch(ctx, timeout, ServiceOperational, onDevice)
}

// watch is the shared implementation for WatchCommissionable and WatchOperational.
func (b *MDNSBrowser) watch(ctx context.Context, timeout time.Duration, svcType ServiceType, onDevice func(*Device) (done bool)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	entries := make(chan *zeroconf.ServiceEntry)

	done := make(chan error, 1)
	go func() {
		done <- b.resolver.Browse(ctx, string(svcType), "local.", entries)
	}()

	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				// Channel closed — browse finished without finding the target.
				if err := <-done; err != nil {
					return fmt.Errorf("browsing %s: %w", svcType, err)
				}
				return nil
			}
			dev := entryToDevice(entry, svcType)
			if onDevice(dev) {
				// Caller found what it needed — cancel the browse and return.
				cancel()
				// Drain the entries channel so the Browse goroutine exits cleanly.
				for range entries {
				}
				<-done
				return nil
			}
		case err := <-done:
			// Browse returned (context timeout or transport error); drain channel.
			for range entries {
			}
			if err != nil {
				return fmt.Errorf("browsing %s: %w", svcType, err)
			}
			return nil
		}
	}
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
	dev.IPs = append(dev.IPs, entry.AddrIPv4...)
	dev.IPs = append(dev.IPs, entry.AddrIPv6...)

	dev.parseTXTRecords(entry.Text)
	return dev
}
