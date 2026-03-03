// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package accesscontrol implements the Matter Access Control cluster (0x001F).
package accesscontrol

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Access Control.
	ID uint32 = 0x001F
	// Name is the CLI-friendly cluster name.
	Name = "AccessControl"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Access Control"
)

// Attribute IDs.
const (
	AttrACL                  uint32 = 0x0000
	AttrExtension            uint32 = 0x0001
	AttrSubjectsPerEntry     uint32 = 0x0002
	AttrTargetsPerEntry      uint32 = 0x0003
	AttrEntriesPerFabric     uint32 = 0x0004
)

// AccessControlEntry represents a single ACL entry.
type AccessControlEntry struct {
	Privilege  uint8    `tlv:"1,uint"`
	AuthMode   uint8    `tlv:"2,uint"`
	Subjects   []uint64 `tlv:"3,array"`
	Targets    []Target `tlv:"4,array"`
	FabricIndex uint8   `tlv:"254,uint"`
}

// Target represents a target within an ACL entry.
type Target struct {
	Cluster    *uint32  `tlv:"0,uint"`
	Endpoint   *uint16  `tlv:"1,uint"`
	DeviceType *uint32  `tlv:"2,uint"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrACL, Name: "ACL", DisplayName: "ACL", Type: "list[struct]", Readable: true, Writable: true},
			{ID: AttrExtension, Name: "Extension", DisplayName: "Extension", Type: "list[struct]", Readable: true, Writable: true, Optional: true},
			{ID: AttrSubjectsPerEntry, Name: "SubjectsPerAccessControlEntry", DisplayName: "SubjectsPerAccessControlEntry", Type: "uint16", Readable: true},
			{ID: AttrTargetsPerEntry, Name: "TargetsPerAccessControlEntry", DisplayName: "TargetsPerAccessControlEntry", Type: "uint16", Readable: true},
			{ID: AttrEntriesPerFabric, Name: "AccessControlEntriesPerFabric", DisplayName: "AccessControlEntriesPerFabric", Type: "uint16", Readable: true},
		},
	})
}
