// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package store provides persistent and in-memory storage for matter-cli
// fabrics, commissioned devices, sessions, and configuration.
package store

import "time"

// Fabric represents a Matter fabric with its root certificate and identity.
type Fabric struct {
	ID            uint64    `json:"id"`
	Label         string    `json:"label"`
	RootCertPEM   string    `json:"root_cert_pem"`
	ICACertPEM    string    `json:"ica_cert_pem"`
	PrivateKeyPEM string    `json:"private_key_pem"`
	VendorID      uint16    `json:"vendor_id"`
	FabricIndex   uint8     `json:"fabric_index"`
	CreatedAt     time.Time `json:"created_at"`
}

// Node represents a commissioned Matter device within a fabric.
type Node struct {
	ID                   uint64     `json:"id"`
	FabricID             uint64     `json:"fabric_id"`
	Name                 string     `json:"name"`
	VendorID             uint16     `json:"vendor_id"`
	ProductID            uint16     `json:"product_id"`
	SpecificationVersion uint32     `json:"specification_version,omitempty"`
	SoftwareVersion      uint32     `json:"software_version,omitempty"`
	SerialNumber         string     `json:"serial_number,omitempty"`
	Endpoints            []Endpoint `json:"endpoints"`
	LastAddress          string     `json:"last_address"`
	LastSeen             time.Time  `json:"last_seen"`
}

// Endpoint represents a single endpoint on a Matter node.
type Endpoint struct {
	ID          uint16       `json:"id"`
	DeviceTypes []DeviceType `json:"device_types"`
	Clusters    []ClusterRef `json:"clusters"`
}

// DeviceType identifies a Matter device type and its revision.
type DeviceType struct {
	ID       uint32 `json:"id"`
	Revision uint16 `json:"revision"`
}

// ClusterRef is a reference to a cluster on an endpoint, including its side
// (client or server).
type ClusterRef struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
	Side string `json:"side"`
	// Attributes holds the attribute IDs this cluster instance advertised in
	// its AttributeList (0xFFFB), as read live from the device by
	// `matter cluster discover` or `matter tree -L 3`/`-L 4`. It is a cache
	// used to scope shell completion to what the device actually implements;
	// a nil slice means the list has never been read, not that the cluster
	// has no attributes.
	//
	// omitempty collapses a successfully-read but empty list back to nil on
	// the next load, so it reads as "never read" and completion falls back to
	// the full spec list. That is the safe direction to degrade — offering
	// every attribute beats offering none — and the case is close to
	// unreachable in practice, since the spec requires every cluster to
	// expose the global attributes (0xFFF8-0xFFFD). The alternative, dropping
	// omitempty, would write "attributes":null into every cluster of every
	// record to preserve a distinction nothing can currently act on.
	Attributes []uint32 `json:"attributes,omitempty"`
}

// IsServer reports whether the reference is to the cluster's server side.
//
// An empty Side counts as server: records written before the field was
// populated carry no side, and every one of them described a server cluster.
func (c ClusterRef) IsServer() bool {
	return c.Side == "server" || c.Side == ""
}

// ResumptionInfo holds CASE session resumption data for a peer node.
type ResumptionInfo struct {
	PeerNodeID        uint64 `json:"peer_node_id"`
	ResumptionID      []byte `json:"resumption_id"`
	SharedSecret      []byte `json:"shared_secret"`
	CASESessionParams []byte `json:"case_session_params"`
}
