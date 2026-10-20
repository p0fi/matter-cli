// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/p0fi/matter-cli/internal/discovery"
	"github.com/p0fi/matter-cli/internal/protocol"
)

const (
	// storedAddrCASETimeout caps the first CASE attempt against a node's stored
	// address. A stale address never answers, so without a cap the attempt
	// would drain the caller's whole budget (30 s by default) and leave nothing
	// for rediscovery and the retry. A reachable device completes CASE well
	// within this; a slow one that misses it is rediscovered at the same
	// address and retried with the remaining budget.
	storedAddrCASETimeout = 10 * time.Second

	// rediscoveryTimeout bounds the operational mDNS lookup that runs after
	// the first CASE attempt fails.
	rediscoveryTimeout = 10 * time.Second
)

// caseDialFunc establishes a CASE session to nodeID at addr.
type caseDialFunc func(ctx context.Context, addr string, nodeID uint64) (*protocol.Session, error)

// operationalResolveFunc looks up nodeID's current "host:port" address via
// operational discovery.
type operationalResolveFunc func(ctx context.Context, nodeID uint64, timeout time.Duration) (string, error)

// ConnectCASEWithRediscovery establishes a CASE session to nodeID, starting
// with the stored address addr. If that attempt fails, the node's current
// address is looked up via operational mDNS discovery and CASE is retried
// there. onRediscovered, if non-nil, is called with the rediscovered address
// before the retry so callers can persist it even if the retry fails.
//
// It returns the session and the address it was established on.
func (c *Controller) ConnectCASEWithRediscovery(
	ctx context.Context, addr string, nodeID uint64, onRediscovered func(addr string),
) (*protocol.Session, string, error) {
	return connectWithRediscovery(ctx, c.ConnectCASE, c.resolveOperationalAddr, addr, nodeID, onRediscovered)
}

func connectWithRediscovery(
	ctx context.Context,
	dial caseDialFunc,
	resolve operationalResolveFunc,
	addr string,
	nodeID uint64,
	onRediscovered func(addr string),
) (*protocol.Session, string, error) {
	firstCtx, cancel := context.WithTimeout(ctx, storedAddrCASETimeout)
	session, err := dial(firstCtx, addr, nodeID)
	cancel()
	if err == nil {
		return session, addr, nil
	}
	if ctx.Err() != nil {
		// The caller gave up (Ctrl+C or its own deadline); don't keep going.
		return nil, "", err
	}

	slog.Info("CASE failed with stored address, attempting operational rediscovery",
		"node", nodeID, "addr", addr, "err", err)
	newAddr, rediscErr := resolve(ctx, nodeID, rediscoveryTimeout)
	if rediscErr != nil {
		return nil, "", fmt.Errorf("%w (operational rediscovery failed: %w)", err, rediscErr)
	}
	slog.Info("rediscovered node via operational mDNS", "node", nodeID, "old", addr, "new", newAddr)
	if onRediscovered != nil {
		onRediscovered(newAddr)
	}

	session, err = dial(ctx, newAddr, nodeID)
	if err != nil {
		return nil, "", fmt.Errorf("after rediscovery at %s: %w", newAddr, err)
	}
	return session, newAddr, nil
}

// resolveOperationalAddr looks up nodeID on this controller's fabric via
// operational mDNS discovery and returns its preferred "host:port" address.
func (c *Controller) resolveOperationalAddr(ctx context.Context, nodeID uint64, timeout time.Duration) (string, error) {
	compressedFabricID := c.CompressedFabricID()
	if len(compressedFabricID) == 0 {
		return "", fmt.Errorf("controller has no fabric identity")
	}
	dev, err := discovery.NewMDNSBrowser().ResolveOperational(ctx, compressedFabricID, nodeID, timeout)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(pickBestIP(dev.IPs).String(), strconv.Itoa(dev.Port)), nil
}

// pickBestIP selects the preferred IP from a list: global/ULA IPv6 first,
// then IPv4, then link-local IPv6 (last resort — link-local requires a zone
// ID to be routable, which mDNS responses don't always provide).
// Returns the first element if none match the categories above.
func pickBestIP(ips []net.IP) net.IP {
	if len(ips) == 0 {
		return nil
	}
	var ipv6Global, ipv4, ipv6LinkLocal net.IP
	for _, ip := range ips {
		switch {
		case ip.To4() != nil:
			if ipv4 == nil {
				ipv4 = ip
			}
		case ip.IsLinkLocalUnicast():
			if ipv6LinkLocal == nil {
				ipv6LinkLocal = ip
			}
		case ipv6Global == nil:
			ipv6Global = ip
		}
	}
	switch {
	case ipv6Global != nil:
		return ipv6Global
	case ipv4 != nil:
		return ipv4
	case ipv6LinkLocal != nil:
		return ipv6LinkLocal
	}
	return ips[0]
}
