// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

// dnssdResolver uses macOS's native dns-sd command (backed by mDNSResponder)
// for mDNS service discovery. The grandcat/zeroconf Go library cannot receive
// mDNS multicast packets on macOS because mDNSResponder monopolizes UDP port
// 5353 — Go's net.ListenMulticastUDP receives zero traffic. The dns-sd tool
// communicates with mDNSResponder via IPC and works reliably.
type dnssdResolver struct {
	// command builds the dns-sd invocation. Nil means the real dns-sd binary;
	// tests substitute a fake that mimics its never-exiting behaviour.
	command func(ctx context.Context, args ...string) *exec.Cmd
}

// dnssdStepTimeout caps each dns-sd -L / -G lookup in case the record never
// arrives.
const dnssdStepTimeout = 5 * time.Second

// dnssdSettle is how long to keep reading after the first answer line so that
// records delivered in the same burst (the TXT line after -L's answer, the
// other address family for -G) are captured before dns-sd is stopped.
const dnssdSettle = 250 * time.Millisecond

func (d *dnssdResolver) cmd(ctx context.Context, args ...string) *exec.Cmd {
	if d.command != nil {
		return d.command(ctx, args...)
	}
	return exec.CommandContext(ctx, "dns-sd", args...)
}

func (d *dnssdResolver) Browse(ctx context.Context, service, domain string, entries chan<- *zeroconf.ServiceEntry) error {
	defer close(entries)

	// dns-sd expects service without trailing dot and domain without trailing dot.
	service = strings.TrimSuffix(service, ".")
	domain = strings.TrimSuffix(domain, ".")

	cmd := d.cmd(ctx, "-B", service, domain)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("dns-sd stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting dns-sd: %w", err)
	}

	// Track already-seen instance names to avoid duplicate lookups.
	seen := make(map[string]bool)

	// Resolve instances concurrently so the browse scanner is not blocked
	// waiting 5 seconds per dns-sd -L / -G lookup. Each new instance spawns
	// a goroutine; results are sent to the entries channel as they complete.
	var wg sync.WaitGroup

	// Limit concurrent resolve goroutines to avoid spawning unbounded
	// dns-sd subprocesses on networks with many Matter instances.
	sem := make(chan struct{}, 8)

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		// Parse browse output lines like:
		//  0:02:37.133  Add        3  14 local.  _matter._tcp.  InstanceName
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		action := fields[1]
		if action != "Add" {
			continue
		}

		// Instance name is everything from field 6 onward (may contain spaces).
		instanceName := strings.Join(fields[6:], " ")
		instanceName = strings.TrimSpace(instanceName)
		if instanceName == "" || seen[instanceName] {
			continue
		}
		seen[instanceName] = true

		slog.Debug("dns-sd: browse found instance", "name", instanceName)

		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			entry, err := d.resolveInstance(ctx, name, service, domain)
			if err != nil {
				slog.Debug("dns-sd: resolve failed, skipping", "name", name, "err", err)
				return
			}
			select {
			case entries <- entry:
			case <-ctx.Done():
			}
		}(instanceName)
	}

	scanErr := scanner.Err()

	// Wait for all in-flight resolutions before closing the entries channel.
	wg.Wait()

	// Kill the browse process (context cancellation does this via CommandContext).
	// Suppress errors caused by context cancellation/deadline, but surface
	// unexpected subprocess failures and scanner errors.
	waitErr := cmd.Wait()
	if scanErr != nil && ctx.Err() == nil {
		return fmt.Errorf("reading dns-sd browse output: %w", scanErr)
	}
	if waitErr != nil && ctx.Err() == nil {
		return fmt.Errorf("waiting for dns-sd browse: %w", waitErr)
	}
	return nil
}

// resolveInstance looks up a specific service instance to get its hostname,
// port, TXT records, and IP addresses. It runs dns-sd -L (lookup) and then
// dns-sd -G (getaddrinfo), stopping each as soon as it has answered.
func (d *dnssdResolver) resolveInstance(ctx context.Context, instance, service, domain string) (*zeroconf.ServiceEntry, error) {
	// Step 1: Lookup (dns-sd -L) — get hostname, port, TXT records.
	lookupCtx, lookupCancel := context.WithTimeout(ctx, dnssdStepTimeout)
	defer lookupCancel()

	out, err := d.runUntilAnswer(lookupCtx, isLookupAnswer, "-L", instance, service, domain)
	if err != nil {
		return nil, fmt.Errorf("dns-sd -L: %w", err)
	}

	hostname, port, txtRecords := parseLookupOutput(out)
	if hostname == "" || port == 0 {
		return nil, fmt.Errorf("dns-sd -L: could not parse hostname/port from output")
	}

	slog.Debug("dns-sd: lookup result", "instance", instance, "host", hostname, "port", port)

	// Step 2: Get address (dns-sd -G) — resolve hostname to IP.
	addrCtx, addrCancel := context.WithTimeout(ctx, dnssdStepTimeout)
	defer addrCancel()

	ipv4s, ipv6s := d.resolveHostname(addrCtx, hostname)

	entry := zeroconf.NewServiceEntry(instance, service, domain)
	entry.HostName = hostname
	entry.Port = port
	entry.AddrIPv4 = ipv4s
	entry.AddrIPv6 = ipv6s
	entry.Text = txtRecords
	return entry, nil
}

// runUntilAnswer runs dns-sd with args and returns its output. dns-sd -L and
// -G never exit on their own — they print answers and keep listening until
// killed — so waiting for the process to finish always costs the full
// timeout. Instead, stop once isAnswer has matched a line and dnssdSettle has
// passed, or when ctx ends.
func (d *dnssdResolver) runUntilAnswer(ctx context.Context, isAnswer func(line string) bool, args ...string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := d.cmd(ctx, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting dns-sd: %w", err)
	}

	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	var out strings.Builder
	settle := time.NewTimer(dnssdSettle)
	settle.Stop() // armed by the first answer line
	defer settle.Stop()
	answered := false
read:
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				break read
			}
			out.WriteString(line)
			out.WriteByte('\n')
			if !answered && isAnswer(line) {
				answered = true
				settle.Reset(dnssdSettle)
			}
		case <-settle.C:
			break read
		case <-ctx.Done():
			break read
		}
	}

	// Kill dns-sd and let the reader observe EOF before reaping the process;
	// Wait must not run while the pipe is still being read. The exit status is
	// meaningless here because we killed it.
	cancel()
	for range lines {
	}
	_ = cmd.Wait()
	return out.String(), nil
}

// isLookupAnswer reports whether a dns-sd -L output line carries the resolved
// host and port.
func isLookupAnswer(line string) bool {
	return strings.Contains(line, "can be reached at")
}

// parseGetAddrLine extracts the address from a dns-sd -G "Add" line, e.g.
//
//	10:21:04.693  Add  40000002  12  host.local.  192.168.1.5  120
//
// It returns nil for any other line.
func parseGetAddrLine(line string) net.IP {
	fields := strings.Fields(line)
	if len(fields) < 6 || fields[1] != "Add" {
		return nil
	}
	// Address is the second-to-last field (before TTL).
	return net.ParseIP(fields[len(fields)-2])
}

// parseLookupOutput parses dns-sd -L output to extract hostname, port, and TXT records.
//
// Example output:
//
//	Lookup Instance._matter._tcp.local
//	 ...
//	 Instance._matter._tcp.local. can be reached at hostname.local.:5540 (interface 14)
//	  T=2
func parseLookupOutput(output string) (hostname string, port int, txt []string) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		// Parse "can be reached at hostname.local.:PORT"
		if strings.Contains(line, "can be reached at") {
			parts := strings.Split(line, "can be reached at")
			if len(parts) < 2 {
				continue
			}
			addrPart := strings.TrimSpace(parts[1])
			// addrPart looks like: "hostname.local.:5540 (interface 14)"
			// Remove the "(interface ...)" suffix.
			if idx := strings.Index(addrPart, "("); idx > 0 {
				addrPart = strings.TrimSpace(addrPart[:idx])
			}
			// Split host:port. The hostname has a trailing dot.
			lastColon := strings.LastIndex(addrPart, ":")
			if lastColon < 0 {
				continue
			}
			hostname = strings.TrimSuffix(addrPart[:lastColon], ".")
			if p, err := strconv.Atoi(addrPart[lastColon+1:]); err == nil {
				port = p
			}
		}

		// Parse TXT records. They appear as space-separated key=value pairs
		// or individual lines starting with a key.
		if strings.HasPrefix(line, "T=") || strings.Contains(line, "=") {
			// Split on space to handle multiple records on one line.
			for _, kv := range strings.Fields(line) {
				if strings.Contains(kv, "=") {
					txt = append(txt, kv)
				}
			}
		}
	}
	return hostname, port, txt
}

// resolveHostname resolves a hostname to IPv4 and IPv6 addresses using dns-sd -G.
func (d *dnssdResolver) resolveHostname(ctx context.Context, hostname string) (ipv4s []net.IP, ipv6s []net.IP) {
	isAnswer := func(line string) bool { return parseGetAddrLine(line) != nil }
	out, err := d.runUntilAnswer(ctx, isAnswer, "-G", "v4v6", hostname+".")
	if err != nil {
		slog.Debug("dns-sd -G failed", "host", hostname, "err", err)
	}

	for _, line := range strings.Split(out, "\n") {
		ip := parseGetAddrLine(line)
		if ip == nil {
			continue
		}
		if ip.To4() != nil {
			ipv4s = append(ipv4s, ip)
		} else {
			ipv6s = append(ipv6s, ip)
		}
	}

	// If dns-sd didn't give us an address, try standard Go resolution as fallback.
	// Skip the fallback when ctx is already done to avoid blocking after cancellation.
	if len(ipv4s) == 0 && len(ipv6s) == 0 && ctx.Err() == nil {
		if addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname); err == nil {
			for _, addr := range addrs {
				ip := addr.IP
				if ip == nil {
					continue
				}
				if ip.To4() != nil {
					ipv4s = append(ipv4s, ip)
				} else {
					ipv6s = append(ipv6s, ip)
				}
			}
		}
	}
	return ipv4s, ipv6s
}
