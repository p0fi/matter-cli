// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/store"
)

// attributeFlagUsage and writeAttributeFlagUsage document the --attribute flag.
// Completion offers the attributes the target device advertises (once
// `matter cluster discover` has cached them), so both spell out the raw-ID
// escape hatch for attributes a device implements without advertising.
const (
	attributeFlagUsage      = "attribute name, or raw ID (0x0006 or 6) to bypass the advertised-attribute list"
	writeAttributeFlagUsage = "attribute name, or raw ID (0x0006 or 6) of a spec-known attribute"
)

// attributeEscapeHatchHelp is appended to the long help of read and subscribe.
const attributeEscapeHatchHelp = `Completion offers the attributes this device advertises in its AttributeList,
once ` + "`matter cluster discover`" + ` (or ` + "`matter tree -L 3`" + `) has cached them; run either
again to refresh a stale cache. Devices sometimes under-report that list, so a
raw attribute ID — hex (0x0006) or decimal (6) — is always accepted and always
bypasses the filter, including for attributes absent from the spec registry
(their value is printed as a raw TLV dump).`

// writeAttributeEscapeHatchHelp is the write-command variant: writes need the
// attribute's type up front to encode a value, so the raw-ID form only covers
// attributes the spec registry knows.
const writeAttributeEscapeHatchHelp = `Completion offers the writable attributes this device advertises in its
AttributeList, once ` + "`matter cluster discover`" + ` (or ` + "`matter tree -L 3`" + `) has cached them.
A raw attribute ID — hex (0x0006) or decimal (6) — bypasses that filter, but
only for attributes the spec registry knows: encoding a value into TLV requires
the attribute's type, which an unknown ID does not supply.`

// parseAttributeID parses a raw attribute ID written as hex ("0x0006", with
// either case for the prefix and digits) or decimal ("6"). It reports false for
// anything that is not a bare numeric literal, so attribute names always take
// precedence over the numeric escape hatch.
func parseAttributeID(s string) (uint32, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if lower := strings.ToLower(s); strings.HasPrefix(lower, "0x") {
		v, err := strconv.ParseUint(lower[2:], 16, 32)
		if err != nil {
			return 0, false
		}
		return uint32(v), true
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}

// resolveCluster resolves a cluster selector — a name or a raw numeric ID
// ("0x0006" or "6") — against the registry.
//
// Names win over IDs, matching resolveReadableAttribute: no registered cluster
// is named as a bare number, so the two namespaces cannot collide.
func resolveCluster(selector string) (*clusters.ClusterInfo, error) {
	if cl, ok := clusters.Global.ClusterByName(selector); ok {
		return cl, nil
	}
	if id, ok := parseAttributeID(selector); ok {
		if cl, ok := clusters.Global.ClusterByID(id); ok {
			return cl, nil
		}
		return nil, fmt.Errorf("unknown cluster 0x%04X", id)
	}
	return nil, fmt.Errorf("unknown cluster %q", selector)
}

// resolveReadableAttribute resolves an attribute selector — a name or a raw
// numeric ID — for read and subscribe. Names are matched against the codegen
// registry first; a numeric ID that the registry knows about resolves to the
// full spec definition, and a numeric ID it has never heard of resolves to a
// synthetic AttributeInfo with an empty Type.
//
// The synthetic entry is deliberate: the TLV wire format self-describes its
// element type, so decodeTLVValue renders an unknown attribute's value without
// any type hint. This is the escape hatch for attributes a device implements
// but does not advertise in its AttributeList, and for manufacturer-specific
// attributes outside the spec entirely.
func resolveReadableAttribute(cl *clusters.ClusterInfo, selector string) (*clusters.AttributeInfo, error) {
	if attr, ok := clusters.Global.AttributeByName(cl.ID, selector); ok {
		return attr, nil
	}
	id, ok := parseAttributeID(selector)
	if !ok {
		return nil, unknownAttributeError(cl, selector)
	}
	if attr, ok := clusters.Global.AttributeByID(cl.ID, id); ok {
		return attr, nil
	}
	name := fmt.Sprintf("0x%04X", id)
	return &clusters.AttributeInfo{
		ID:          id,
		Name:        name,
		DisplayName: name,
		Readable:    true,
	}, nil
}

// resolveWritableAttribute resolves an attribute selector for write. Unlike
// resolveReadableAttribute it refuses IDs the registry does not know: encoding
// a CLI string into TLV requires the target type up front, and there is no type
// to encode against for an attribute outside the spec registry.
func resolveWritableAttribute(cl *clusters.ClusterInfo, selector string) (*clusters.AttributeInfo, error) {
	if attr, ok := clusters.Global.AttributeByName(cl.ID, selector); ok {
		return attr, nil
	}
	id, ok := parseAttributeID(selector)
	if !ok {
		return nil, unknownAttributeError(cl, selector)
	}
	attr, ok := clusters.Global.AttributeByID(cl.ID, id)
	if !ok {
		return nil, fmt.Errorf(
			"attribute 0x%04X is not in the spec registry for cluster %q, so its type is unknown and a write cannot be encoded\n\n"+
				"Reads do not need a type and still work:\n  matter cluster read --cluster %s --attribute %s",
			id, cl.Name, cl.Name, selector)
	}
	return attr, nil
}

// unknownAttributeError builds the error returned when a selector is neither a
// known attribute name nor a numeric ID, pointing at `cluster discover` since a
// stale completion cache is the most likely reason a name went missing.
func unknownAttributeError(cl *clusters.ClusterInfo, selector string) error {
	return fmt.Errorf(
		"unknown attribute %q in cluster %q\n\n"+
			"Pass a raw attribute ID (e.g. 0x0006) to bypass name lookup, or refresh the cached attribute list:\n  matter cluster discover",
		selector, cl.Name)
}

// attrListReader reads a single cluster instance's AttributeList (0xFFFB) and
// returns the attribute IDs it advertises. Both `cluster discover` and
// `tree -L 3` bind it to a live CASE session or session-daemon connection;
// tests bind it to a fake so the traversal and cache-write logic can be
// exercised without a device.
type attrListReader func(ctx context.Context, endpoint uint16, clusterID uint32) ([]uint32, error)

// persistAttributeCache saves the attribute lists discovered on node without
// disturbing the rest of its record.
//
// The node passed in was loaded before the CASE session opened, and the session
// itself writes to the same record: connectToNode (and the daemon) refresh
// LastSeen on every connect, and LastAddress whenever mDNS rediscovery finds a
// device at a new IP. Saving the pre-session snapshot would roll both back —
// most damagingly the rediscovered address, leaving the store pointing at an
// address already known to be dead. So reload the current record and copy only
// the attribute lists onto it.
func persistAttributeCache(fabricID uint64, node *store.Node) error {
	current, err := loadNodeForCompletion(fabricID, node.ID)
	if err != nil {
		return fmt.Errorf("reloading node %d: %w", node.ID, err)
	}
	for _, ep := range node.Endpoints {
		for _, cl := range ep.Clusters {
			if cl.Attributes == nil {
				continue
			}
			applyAttributeList(current, ep.ID, cl.ID, cl.Attributes)
		}
	}
	return persistNode(fabricID, current)
}

// recordAttrListResult write-throughs one AttributeList read into node's
// completion cache and reports whether the cache changed.
//
// A non-nil readErr is deliberately a no-op: the cluster keeps whatever list was
// cached before. A device that is busy, slow, or momentarily unreachable should
// not cost the user a list that was already verified once — a stale list still
// scopes completion better than no list at all.
func recordAttrListResult(node *store.Node, endpoint uint16, clusterID uint32, attrIDs []uint32, readErr error) bool {
	if readErr != nil {
		return false
	}
	return applyAttributeList(node, endpoint, clusterID, attrIDs)
}

// applyAttributeList caches the attribute IDs read from a cluster instance on
// the given node, replacing whatever was cached before. It reports whether a
// matching endpoint/cluster record existed to update.
//
// Callers must only invoke this after a successful AttributeList read: a failed
// read leaves the previous cache untouched, because a stale-but-once-verified
// list is more useful for completion than reverting to no filtering at all.
func applyAttributeList(node *store.Node, endpointID uint16, clusterID uint32, attrIDs []uint32) bool {
	if node == nil {
		return false
	}
	for ei := range node.Endpoints {
		ep := &node.Endpoints[ei]
		if ep.ID != endpointID {
			continue
		}
		for ci := range ep.Clusters {
			if ep.Clusters[ci].ID != clusterID {
				continue
			}
			ep.Clusters[ci].Attributes = attrIDs
			return true
		}
		return false
	}
	return false
}
