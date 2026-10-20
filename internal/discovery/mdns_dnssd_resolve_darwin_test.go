// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

// fakeDNSSD returns a command builder that re-executes the test binary as a
// stand-in for dns-sd (see TestFakeDNSSDProcess). Like the real tool, the fake
// prints its answer immediately and then never exits on its own.
func fakeDNSSD(instance, host, ip string) func(context.Context, ...string) *exec.Cmd {
	return func(ctx context.Context, args ...string) *exec.Cmd {
		cmdArgs := append([]string{"-test.run=^TestFakeDNSSDProcess$", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cmdArgs...)
		cmd.Env = append(os.Environ(),
			"FAKE_DNSSD=1",
			"FAKE_DNSSD_INSTANCE="+instance,
			"FAKE_DNSSD_HOST="+host,
			"FAKE_DNSSD_IP="+ip,
		)
		return cmd
	}
}

// TestFakeDNSSDProcess is not a real test: it is the body of the fake dns-sd
// process started by fakeDNSSD, and is skipped in normal runs.
func TestFakeDNSSDProcess(t *testing.T) {
	if os.Getenv("FAKE_DNSSD") != "1" {
		t.Skip("helper process for fakeDNSSD")
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	instance := os.Getenv("FAKE_DNSSD_INSTANCE")
	host := os.Getenv("FAKE_DNSSD_HOST")
	ip := os.Getenv("FAKE_DNSSD_IP")

	switch args[0] {
	case "-B":
		fmt.Printf("Browsing for %s.%s\n", args[1], args[2])
		fmt.Println("10:00:00.000  ...STARTING...")
		fmt.Println("Timestamp     A/R    Flags  if Domain               Service Type         Instance Name")
		fmt.Printf("10:00:00.001  Add        2  14 local.               %s.        %s\n", args[1], instance)
	case "-L":
		fmt.Printf("Lookup %s.%s.%s\n", args[1], args[2], args[3])
		fmt.Println("10:00:00.000  ...STARTING...")
		fmt.Printf("10:00:00.002  %s.%s.%s. can be reached at %s.:5540 (interface 14)\n", args[1], args[2], args[3], host)
		fmt.Println(" SII=5000 SAI=300 T=0")
	case "-G":
		fmt.Println("10:00:00.000  ...STARTING...")
		fmt.Println("Timestamp     A/R  Flags         IF  Hostname                               Address                                      TTL")
		fmt.Printf("10:00:00.003  Add  40000002      14  %s.  %s  120\n", host, ip)
	}
	// The real dns-sd keeps listening until killed.
	time.Sleep(time.Minute)
	os.Exit(0)
}

// TestDNSSDResolver_ResolveOperationalWithinBudget is the regression test for
// rediscovery never finding a device on macOS: dns-sd -L and -G never exit, so
// waiting for them cost a fixed 5 s per step and a 5 s rediscovery budget
// always expired before any IP was resolved.
func TestDNSSDResolver_ResolveOperationalWithinBudget(t *testing.T) {
	const instance = "AABBCCDD11223344-0000000000000019"
	browser := newMDNSBrowserWithResolver(&dnssdResolver{
		command: fakeDNSSD(instance, "AABBCCDD11223344.local", "192.168.50.23"),
	})
	fabricID := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0x11, 0x22, 0x33, 0x44}

	start := time.Now()
	dev, err := browser.ResolveOperational(context.Background(), fabricID, 0x19, 5*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ResolveOperational: %v (after %s)", err, elapsed)
	}
	if len(dev.IPs) != 1 || !dev.IPs[0].Equal(net.ParseIP("192.168.50.23")) {
		t.Errorf("IPs = %v, want [192.168.50.23]", dev.IPs)
	}
	if dev.Port != 5540 {
		t.Errorf("Port = %d, want 5540", dev.Port)
	}
	if elapsed > 3*time.Second {
		t.Errorf("resolution took %s; dns-sd lookups must stop once answered", elapsed)
	}
}

func TestDNSSDResolver_ResolveInstanceStopsOnceAnswered(t *testing.T) {
	d := &dnssdResolver{command: fakeDNSSD("inst", "node.local", "fd00::19")}

	start := time.Now()
	entry, err := d.resolveInstance(context.Background(), "inst", "_matter._tcp", "local")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("resolveInstance: %v", err)
	}
	if entry.HostName != "node.local" || entry.Port != 5540 {
		t.Errorf("host:port = %s:%d, want node.local:5540", entry.HostName, entry.Port)
	}
	if len(entry.AddrIPv6) != 1 || !entry.AddrIPv6[0].Equal(net.ParseIP("fd00::19")) {
		t.Errorf("AddrIPv6 = %v, want [fd00::19]", entry.AddrIPv6)
	}
	if len(entry.Text) == 0 {
		t.Error("expected TXT records from the line following the answer")
	}
	if elapsed > 2*time.Second {
		t.Errorf("resolveInstance took %s; want well under the %s per-step timeout", elapsed, dnssdStepTimeout)
	}
}

func TestParseGetAddrLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"ipv4", "10:21:04.693  Add  40000002  12  host.local.  192.168.1.5  120", "192.168.1.5"},
		{"ipv6", "10:21:04.693  Add  40000003  12  host.local.  fd00::5  120", "fd00::5"},
		{"remove", "10:21:04.693  Rmv  40000002  12  host.local.  192.168.1.5  0", ""},
		{"header", "Timestamp     A/R  Flags  IF  Hostname  Address  TTL", ""},
		{"starting", "10:21:04.693  ...STARTING...", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGetAddrLine(tt.line)
			if tt.want == "" {
				if got != nil {
					t.Errorf("parseGetAddrLine = %v, want nil", got)
				}
				return
			}
			if !got.Equal(net.ParseIP(tt.want)) {
				t.Errorf("parseGetAddrLine = %v, want %s", got, tt.want)
			}
		})
	}
}
