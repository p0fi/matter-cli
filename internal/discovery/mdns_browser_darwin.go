// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package discovery

// NewMDNSBrowser returns a new MDNSBrowser that uses macOS's native dns-sd
// command (backed by mDNSResponder) for mDNS service discovery.
//
// On macOS the grandcat/zeroconf Go library cannot receive mDNS multicast
// packets because mDNSResponder monopolizes UDP port 5353 — Go's
// net.ListenMulticastUDP receives zero traffic. The dns-sd tool communicates
// with mDNSResponder via IPC and works reliably.
func NewMDNSBrowser() *MDNSBrowser {
	return &MDNSBrowser{
		resolver: &dnssdResolver{},
	}
}
