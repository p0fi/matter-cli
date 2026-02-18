// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package descriptor implements the Matter Descriptor cluster (0x001D).
package descriptor

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Descriptor.
	ID uint32 = 0x001D
	// Name is the CLI-friendly cluster name.
	Name = "descriptor"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Descriptor"
)

// Attribute IDs.
const (
	AttrDeviceTypeList uint32 = 0x0000
	AttrServerList     uint32 = 0x0001
	AttrClientList     uint32 = 0x0002
	AttrPartsList      uint32 = 0x0003
)

// DeviceType represents a device type entry in the DeviceTypeList attribute.
type DeviceType struct {
	DeviceType uint16 `tlv:"0,uint"`
	Revision   uint16 `tlv:"1,uint"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrDeviceTypeList, Name: "device-type-list", DisplayName: "DeviceTypeList", Type: "list[struct]", Readable: true},
			{ID: AttrServerList, Name: "server-list", DisplayName: "ServerList", Type: "list[cluster-id]", Readable: true},
			{ID: AttrClientList, Name: "client-list", DisplayName: "ClientList", Type: "list[cluster-id]", Readable: true},
			{ID: AttrPartsList, Name: "parts-list", DisplayName: "PartsList", Type: "list[endpoint-no]", Readable: true},
		},
	})
}
