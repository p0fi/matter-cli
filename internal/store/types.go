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
}

// ResumptionInfo holds CASE session resumption data for a peer node.
type ResumptionInfo struct {
	PeerNodeID        uint64 `json:"peer_node_id"`
	ResumptionID      []byte `json:"resumption_id"`
	SharedSecret      []byte `json:"shared_secret"`
	CASESessionParams []byte `json:"case_session_params"`
}
