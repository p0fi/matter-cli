// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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
				ble:  discovery.NewBLEBrowser(scanner),
				mdns: &controllerDiscoverer{browser: discovery.NewMDNSBrowser()},
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
	return &controllerSession{session: session}, nil
}

func (s *bleSessionEstablisher) EstablishCASE(ctx context.Context, addr string, nodeID uint64) (commissioning.Session, error) {
	// CASE always goes over IP. Close the BLE connection if it's still open.
	if s.bleConn != nil {
		slog.Debug("ble: closing BLE connection before CASE over IP")
		s.bleConn.Close()
		s.bleConn = nil
	}

	// The addr from BLE discovery is "ble://...", but for CASE we need to
	// rediscover the device on IP. Use mDNS operational discovery.
	session, err := s.ctrl.ConnectCASE(ctx, addr, nodeID)
	if err != nil {
		return nil, err
	}
	return &controllerSession{session: session}, nil
}

// ─── Auto-detection discoverer ────────────────────────────────────────────────

// autoDiscoverer tries BLE discovery first, falling back to mDNS.
type autoDiscoverer struct {
	ble  *discovery.BLEBrowser
	mdns *controllerDiscoverer
}

func (d *autoDiscoverer) DiscoverCommissionable(ctx context.Context, discriminator uint16) (string, error) {
	// Try BLE first.
	addr, err := d.ble.DiscoverCommissionable(ctx, discriminator)
	if err == nil {
		slog.Debug("auto-discover: found device via BLE", "addr", addr)
		return addr, nil
	}
	slog.Debug("auto-discover: BLE scan failed, falling back to mDNS", "err", err)

	// Fall back to mDNS.
	return d.mdns.DiscoverCommissionable(ctx, discriminator)
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
	// Enable the BLE adapter.
	if err := adapter.Enable(); err != nil {
		return nil, nil, fmt.Errorf("controller: enabling BLE adapter: %w", err)
	}

	// Dial the BLE device and complete the BTP handshake.
	bleConn, err := transport.DialBLE(ctx, adapter, addr)
	if err != nil {
		return nil, nil, fmt.Errorf("controller: %w", err)
	}

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
	session, err := subCtrl.connectPASEWithAddr(ctx, passcode)
	if err != nil {
		bleConn.Close()
		return nil, nil, fmt.Errorf("controller: BLE PASE: %w", err)
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

	// Start the message pump on the BLE connection.
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
