// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package generalcommissioning implements the Matter General Commissioning cluster (0x0030).
package generalcommissioning

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for General Commissioning.
	ID uint32 = 0x0030
	// Name is the CLI-friendly cluster name.
	Name = "general-commissioning"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "General Commissioning"
)

// Attribute IDs.
const (
	AttrBreadcrumb               uint32 = 0x0000
	AttrBasicCommissioningInfo   uint32 = 0x0001
	AttrRegulatoryConfig         uint32 = 0x0002
	AttrLocationCapability       uint32 = 0x0003
	AttrSupportsConcurrentConn   uint32 = 0x0004
)

// Command IDs.
const (
	CmdArmFailSafe              uint32 = 0x00
	CmdArmFailSafeResponse      uint32 = 0x01
	CmdSetRegulatoryConfig      uint32 = 0x02
	CmdSetRegulatoryConfigResp  uint32 = 0x03
	CmdCommissioningComplete    uint32 = 0x04
	CmdCommissioningCompleteRes uint32 = 0x05
)

// ArmFailSafeRequest is the request payload for the ArmFailSafe command.
type ArmFailSafeRequest struct {
	ExpiryLengthSeconds uint16 `tlv:"0,uint"`
	Breadcrumb          uint64 `tlv:"1,uint"`
}

// ArmFailSafeResponse is the response payload for the ArmFailSafe command.
type ArmFailSafeResponse struct {
	ErrorCode uint8  `tlv:"0,uint"`
	DebugText string `tlv:"1,utf8"`
}

// SetRegulatoryConfigRequest is the request payload for the SetRegulatoryConfig command.
type SetRegulatoryConfigRequest struct {
	NewRegulatoryConfig uint8  `tlv:"0,uint"`
	CountryCode         string `tlv:"1,utf8"`
	Breadcrumb          uint64 `tlv:"2,uint"`
}

// SetRegulatoryConfigResponse is the response payload for the SetRegulatoryConfig command.
type SetRegulatoryConfigResponse struct {
	ErrorCode uint8  `tlv:"0,uint"`
	DebugText string `tlv:"1,utf8"`
}

// CommissioningCompleteResponse is the response payload for the CommissioningComplete command.
type CommissioningCompleteResponse struct {
	ErrorCode uint8  `tlv:"0,uint"`
	DebugText string `tlv:"1,utf8"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrBreadcrumb, Name: "breadcrumb", DisplayName: "Breadcrumb", Type: "uint64", Readable: true, Writable: true},
			{ID: AttrBasicCommissioningInfo, Name: "basic-commissioning-info", DisplayName: "BasicCommissioningInfo", Type: "struct", Readable: true},
			{ID: AttrRegulatoryConfig, Name: "regulatory-config", DisplayName: "RegulatoryConfig", Type: "enum8", Readable: true},
			{ID: AttrLocationCapability, Name: "location-capability", DisplayName: "LocationCapability", Type: "enum8", Readable: true},
			{ID: AttrSupportsConcurrentConn, Name: "supports-concurrent-connection", DisplayName: "SupportsConcurrentConnection", Type: "bool", Readable: true},
		},
		Commands: []clusters.CommandInfo{
			{ID: CmdArmFailSafe, Name: "arm-fail-safe", DisplayName: "ArmFailSafe", HasRequest: true, HasResponse: true},
			{ID: CmdSetRegulatoryConfig, Name: "set-regulatory-config", DisplayName: "SetRegulatoryConfig", HasRequest: true, HasResponse: true},
			{ID: CmdCommissioningComplete, Name: "commissioning-complete", DisplayName: "CommissioningComplete", HasResponse: true},
		},
	})
}
