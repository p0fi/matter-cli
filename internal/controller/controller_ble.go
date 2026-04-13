// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package controller

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/p0fi/matter-cli/internal/commissioning"
	"github.com/p0fi/matter-cli/internal/discovery"
	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/transport"
)

// TransportPreference controls which transport is used for commissioning.
type TransportPreference string

const (
	// TransportAuto detects the transport from the QR code's DiscoveryCapabilities
	// field, preferring BLE when the device advertises it.
	TransportAuto TransportPreference = "auto"

	// TransportBLE forces BLE commissioning.
	TransportBLE TransportPreference = "ble"

	// TransportIP forces IP/mDNS commissioning.
	TransportIP TransportPreference = "ip"
)

// NewCommissionerWithTransport assembles a commissioning.Commissioner that uses
// the given transport preference to select between BLE and IP discovery/session
// establishment.
func (c *Controller) NewCommissionerWithTransport(pref TransportPreference, adapter transport.BLEAdapter) *commissioning.Commissioner {
	switch pref {
	case TransportBLE:
		scanner := transport.NewBLEScanner(adapter)
		return &commissioning.Commissioner{
			Discoverer: discovery.NewBLEBrowser(scanner),
			Sessions:   &bleSessionEstablisher{ctrl: c, adapter: adapter},
			Client:     &controllerIMClient{ctrl: c},
			NOCIssuer:  &controllerNOCIssuer{ctrl: c},
		}
	case TransportIP:
		return c.NewCommissioner()
	default: // auto
		scanner := transport.NewBLEScanner(adapter)
		return &commissioning.Commissioner{
			Discoverer: &autoDiscoverer{
				ble:     discovery.NewBLEBrowser(scanner),
				mdns:    &controllerDiscoverer{browser: discovery.NewMDNSBrowser()},
				adapter: adapter,
			},
			Sessions: &autoSessionEstablisher{
				ble: &bleSessionEstablisher{ctrl: c, adapter: adapter},
				ip:  &controllerSessionEstablisher{ctrl: c},
			},
			Client:    &controllerIMClient{ctrl: c},
			NOCIssuer: &controllerNOCIssuer{ctrl: c},
		}
	}
}

// BLEAdapter is the exported type alias so callers don't need to import
// the transport package directly.
type BLEAdapter = transport.BLEAdapter

// ─── BLE session establisher ──────────────────────────────────────────────────

// bleSessionEstablisher implements commissioning.SessionEstablisher for BLE
// commissioning. PASE is established over BLE; CASE always goes over IP.
type bleSessionEstablisher struct {
	ctrl    *Controller
	adapter transport.BLEAdapter

	// bleConn holds the open BLE connection for the PASE session. It is
	// closed after CASE succeeds over IP (per Matter spec §5.5.1).
	bleConn *transport.BLEConn
}

func (s *bleSessionEstablisher) EstablishPASE(ctx context.Context, addr string, passcode uint32) (commissioning.Session, error) {
	bleAddr, err := parseBLEAddress(addr)
	if err != nil {
		return nil, fmt.Errorf("BLE PASE: %w", err)
	}

	session, bleConn, err := s.ctrl.ConnectPASEoverBLE(ctx, s.adapter, bleAddr, passcode)
	if err != nil {
		return nil, err
	}

	s.bleConn = bleConn
	return &controllerSession{session: session, addr: addr}, nil
}

func (s *bleSessionEstablisher) EstablishCASE(ctx context.Context, addr string, nodeID uint64) (commissioning.Session, error) {
	// CASE always goes over IP. We must cleanly transition the controller
	// from the BLE transport back to UDP before attempting CASE.
	if s.bleConn != nil {
		slog.Debug("ble: transitioning controller from BLE to IP for CASE")

		// 1. Stop the controller's message pump that is reading from the
		//    BLE connection. Without this, closing the BLE conn causes
		//    Receive() to return ErrConnClosed in a tight loop.
		if s.ctrl.cancel != nil {
			s.ctrl.cancel()
			<-s.ctrl.done
			s.ctrl.cancel = nil
		}

		// 2. Close the BLE connection now that nothing is reading from it.
		s.bleConn.Close()
		s.bleConn = nil

		// 3. Create a fresh UDP connection so the controller can
		//    communicate over IP for the CASE handshake.
		udpConn, err := transport.NewUDPConn(":0")
		if err != nil {
			return nil, fmt.Errorf("creating UDP connection for CASE: %w", err)
		}
		s.ctrl.conn = udpConn
		// done channel must be re-created so startMessagePump can close it.
		s.ctrl.done = make(chan struct{})

		slog.Debug("ble: controller restored to UDP transport for CASE")
	}

	// The addr from BLE discovery is "ble://...", but for CASE we need to
	// rediscover the device on IP. Use mDNS operational discovery.
	var ipAddr string
	if isBLEAddress(addr) {
		// The operational mDNS instance name has the format
		// "<compressed-fabric-id-hex>-<node-id-hex>", e.g.
		// "8E9462691A5722B9-0000000000000005" for node 5.
		//
		// Build the expected full instance name so we can match exactly.
		// If the compressed fabric ID is not available (no fabric loaded),
		// fall back to matching on the node ID suffix only.
		nodeIDHex := fmt.Sprintf("%016X", nodeID)
		var expectedName string
		var compressedHex string
		if s.ctrl.fabric != nil && len(s.ctrl.fabric.compressedFabricID) == 8 {
			compressedHex = strings.ToUpper(hex.EncodeToString(s.ctrl.fabric.compressedFabricID))
			expectedName = compressedHex + "-" + nodeIDHex
		}

		slog.Debug("ble: discovering device on operational network for CASE",
			"nodeID", nodeID,
			"expectedInstanceName", expectedName,
			"compressedFabricID", compressedHex)

		// Use a single continuous mDNS browse for the full remaining context
		// budget rather than a series of short disconnected 10-second windows.
		// This is critical for Thread commissioning: the device may take
		// 30-120 seconds to attach to the Thread mesh and start advertising
		// on the IP network. A single long browse keeps the multicast socket
		// open the whole time, so we catch the announcement the instant it
		// arrives instead of polling and potentially missing it between windows.
		browser := discovery.NewMDNSBrowser()
		var foundDev *discovery.Device
		watchErr := browser.WatchOperational(ctx, 3*time.Minute, func(dev *discovery.Device) bool {
			if len(dev.IPs) == 0 {
				return false
			}
			upperName := strings.ToUpper(dev.Name)
			ipStrs := make([]string, len(dev.IPs))
			for j, ip := range dev.IPs {
				ipStrs[j] = ip.String()
			}
			slog.Debug("ble: mDNS operational entry",
				"name", dev.Name,
				"host", dev.Host,
				"port", dev.Port,
				"ips", strings.Join(ipStrs, ","))

			if expectedName != "" {
				// Exact match on full instance name (compressed fabric ID + node ID).
				if upperName != expectedName {
					slog.Debug("ble: skipping mDNS entry (name mismatch)",
						"name", dev.Name, "want", expectedName)
					return false
				}
			} else {
				// Fallback: match on node ID suffix only.
				nodeIDSuffix := "-" + nodeIDHex
				if !strings.HasSuffix(upperName, nodeIDSuffix) {
					slog.Debug("ble: skipping mDNS entry (node ID mismatch)",
						"name", dev.Name, "want", nodeIDSuffix)
					return false
				}
			}
			foundDev = dev
			return true // stop the browse
		})
		if watchErr != nil {
			return nil, fmt.Errorf("discovering device on IP for CASE: %w", watchErr)
		}
		if foundDev == nil {
			return nil, fmt.Errorf("no operational device found via mDNS after BLE commissioning (looking for node %d, expected instance name %q)", nodeID, expectedName)
		}

		// Use net.JoinHostPort which handles IPv6 bracket notation
		// automatically (e.g. "[fd48:…]:5540").
		ipAddr = net.JoinHostPort(foundDev.IPs[0].String(), fmt.Sprintf("%d", foundDev.Port))
		slog.Debug("ble: found operational device via mDNS",
			"name", foundDev.Name, "addr", ipAddr)
	} else {
		ipAddr = addr
	}

	session, err := s.ctrl.ConnectCASE(ctx, ipAddr, nodeID)
	if err != nil {
		return nil, err
	}
	return &controllerSession{session: session, addr: ipAddr}, nil
}

// ─── Auto-detection discoverer ────────────────────────────────────────────────

// transportDiscoverer is the common interface used by autoDiscoverer for both
// its BLE and mDNS sub-discoverers. It matches commissioning.DeviceDiscoverer
// but is defined locally so autoDiscoverer can be tested without importing the
// commissioning package in tests.
type transportDiscoverer interface {
	DiscoverCommissionable(ctx context.Context, discriminator uint16, caps commissioning.DiscoveryCapabilities) (string, error)
}

// autoDiscoverer runs BLE and mDNS discovery in parallel and returns
// whichever transport finds the device first.
type autoDiscoverer struct {
	ble     transportDiscoverer
	mdns    transportDiscoverer
	adapter transport.BLEAdapter
}

// bleDiscoveryTimeout caps how long the BLE scan runs when both transports
// are tried in parallel. BLE devices in range are typically found within a
// few seconds; 15 s matches the previous sequential timeout.
const bleDiscoveryTimeout = 15 * time.Second

type discoveryResult struct {
	addr    string
	err     error
	fromBLE bool
}

func (d *autoDiscoverer) DiscoverCommissionable(ctx context.Context, discriminator uint16, caps commissioning.DiscoveryCapabilities) (string, error) {
	// When DiscoveryCapabilities is known (non-zero), skip transports the
	// device does not advertise. This avoids the 15 s BLE-adapter warm-up
	// cost for on-network devices and skips a pointless mDNS browse for
	// BLE-only devices.
	tryBLE := caps == 0 || caps&commissioning.DiscoveryBLE != 0
	tryMDNS := caps == 0 || caps&commissioning.DiscoveryOnNetwork != 0

	if !tryBLE {
		slog.Debug("auto-discover: skipping BLE (not in DiscoveryCapabilities)")
		return d.mdns.DiscoverCommissionable(ctx, discriminator, caps)
	}
	if !tryMDNS {
		slog.Debug("auto-discover: skipping mDNS (not in DiscoveryCapabilities)")
		if err := d.adapter.Enable(); err != nil {
			return "", fmt.Errorf("BLE adapter: %w", err)
		}
		bleCtx, bleCancel := context.WithTimeout(ctx, bleDiscoveryTimeout)
		defer bleCancel()
		return d.ble.DiscoverCommissionable(bleCtx, discriminator, caps)
	}

	// Both transports: run in parallel, return whichever responds first.
	slog.Debug("auto-discover: running BLE and mDNS discovery in parallel")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan discoveryResult, 2)

	// BLE goroutine: enable the adapter first (may block for a few seconds on
	// macOS while CoreBluetooth initialises). Running inside the goroutine
	// means mDNS starts immediately and is not delayed by BLE initialisation.
	go func() {
		if err := d.adapter.Enable(); err != nil {
			slog.Debug("auto-discover: BLE adapter enable failed, skipping BLE", "err", err)
			results <- discoveryResult{err: err, fromBLE: true}
			return
		}
		slog.Debug("auto-discover: BLE scan started")
		bleCtx, bleCancel := context.WithTimeout(ctx, bleDiscoveryTimeout)
		defer bleCancel()
		addr, err := d.ble.DiscoverCommissionable(bleCtx, discriminator, caps)
		if err == nil {
			slog.Debug("auto-discover: found device via BLE", "addr", addr)
		}
		results <- discoveryResult{addr: addr, err: err, fromBLE: true}
	}()

	go func() {
		slog.Debug("auto-discover: mDNS browse started")
		addr, err := d.mdns.DiscoverCommissionable(ctx, discriminator, caps)
		if err == nil {
			slog.Debug("auto-discover: found device via mDNS", "addr", addr)
		}
		results <- discoveryResult{addr: addr, err: err}
	}()

	// Return the first successful result. If both fail, return the mDNS error:
	// it is more actionable than a BLE scan timeout for the typical case where
	// the device is on-network, and makes the error deterministic across runs.
	var bleErr, mdnsErr error
	for range 2 {
		r := <-results
		if r.err == nil {
			cancel() // stop the other goroutine
			return r.addr, nil
		}
		if r.fromBLE {
			bleErr = r.err
		} else {
			mdnsErr = r.err
		}
	}
	if mdnsErr != nil {
		return "", mdnsErr
	}
	return "", bleErr
}

// ─── Auto-detection session establisher ───────────────────────────────────────

// autoSessionEstablisher routes PASE to BLE or IP based on the address format.
type autoSessionEstablisher struct {
	ble *bleSessionEstablisher
	ip  *controllerSessionEstablisher
}

func (s *autoSessionEstablisher) EstablishPASE(ctx context.Context, addr string, passcode uint32) (commissioning.Session, error) {
	if isBLEAddress(addr) {
		return s.ble.EstablishPASE(ctx, addr, passcode)
	}
	return s.ip.EstablishPASE(ctx, addr, passcode)
}

func (s *autoSessionEstablisher) EstablishCASE(ctx context.Context, addr string, nodeID uint64) (commissioning.Session, error) {
	if isBLEAddress(addr) {
		return s.ble.EstablishCASE(ctx, addr, nodeID)
	}
	return s.ip.EstablishCASE(ctx, addr, nodeID)
}

// ─── Controller BLE PASE ──────────────────────────────────────────────────────

// ConnectPASEoverBLE establishes a PASE session with a device over BLE/BTP.
// It creates a temporary sub-controller backed by the BLE transport.
func (c *Controller) ConnectPASEoverBLE(ctx context.Context, adapter transport.BLEAdapter, addr transport.BLEAddress, passcode uint32) (*protocol.Session, *transport.BLEConn, error) {
	slog.Debug("ble-pase: enabling adapter")
	// Enable the BLE adapter.
	if err := adapter.Enable(); err != nil {
		return nil, nil, fmt.Errorf("controller: enabling BLE adapter: %w", err)
	}

	// Dial the BLE device and complete the BTP handshake.
	slog.Debug("ble-pase: dialing device", "addr", addr)
	bleConn, err := transport.DialBLE(ctx, adapter, addr)
	if err != nil {
		return nil, nil, fmt.Errorf("controller: %w", err)
	}
	slog.Debug("ble-pase: BLE connection established")

	// Create a sub-controller using the BLE connection.
	subCtrl, err := NewWithConn(Config{
		Store:    c.store,
		FabricID: c.fabricID,
	}, bleConn)
	if err != nil {
		bleConn.Close()
		return nil, nil, fmt.Errorf("controller: creating BLE sub-controller: %w", err)
	}

	// Set a dummy peer address since BLE is point-to-point.
	subCtrl.mu.Lock()
	subCtrl.peerAddr = &transport.BLEAddr{Address: addr}
	subCtrl.mu.Unlock()

	// Run PASE over the BLE transport.
	slog.Debug("ble-pase: starting PASE handshake")
	session, err := subCtrl.connectPASEWithAddr(ctx, passcode)
	if err != nil {
		// Stop the sub-controller's pump before closing the connection so
		// the pump goroutine exits cleanly instead of logging a receive error.
		if subCtrl.cancel != nil {
			subCtrl.cancel()
			<-subCtrl.done
		}
		bleConn.Close()
		return nil, nil, fmt.Errorf("controller: BLE PASE: %w", err)
	}

	// Stop the sub-controller's message pump before migrating state.
	//
	// connectPASEWithAddr starts a pump goroutine on subCtrl so it can
	// receive PASE messages. That goroutine calls bleConn.Receive(), which
	// reads from the shared btp.Messages() channel. If we start the main
	// controller's pump without stopping subCtrl's first, two goroutines
	// compete on the same channel and randomly steal each other's messages
	// (e.g. an ArmFailsafe response arriving on subCtrl's goroutine is
	// dispatched to an exchange that no longer exists, and the main
	// controller never sees it).
	if subCtrl.cancel != nil {
		slog.Debug("ble-pase: stopping sub-controller message pump")
		subCtrl.cancel()
		<-subCtrl.done
	}

	// Migrate the session and exchange state back to the main controller so
	// that subsequent IM operations (ArmFailsafe, CSR, etc.) work over the
	// BLE transport.
	c.conn = bleConn
	c.sessions = subCtrl.sessions
	c.exchanges = subCtrl.exchanges
	c.exchanges.DefaultSendFunc = c.sendMessage

	c.mu.Lock()
	c.peerAddr = &transport.BLEAddr{Address: addr}
	c.mu.Unlock()

	// Stop the main controller's existing pump (if any) then start a fresh
	// one backed by the BLE connection.
	if c.cancel != nil {
		c.cancel()
		<-c.done
	}
	c.startMessagePump()

	return session, bleConn, nil
}

// ─── Address helpers ──────────────────────────────────────────────────────────

// isBLEAddress returns true if addr looks like a BLE address ("ble://...").
func isBLEAddress(addr string) bool {
	return strings.HasPrefix(addr, "ble://")
}

// parseBLEAddress extracts the opaque BLE address from a "ble://<addr>" string.
func parseBLEAddress(addr string) (transport.BLEAddress, error) {
	if !strings.HasPrefix(addr, "ble://") {
		return "", fmt.Errorf("not a BLE address: %q", addr)
	}
	return transport.BLEAddress(strings.TrimPrefix(addr, "ble://")), nil
}
