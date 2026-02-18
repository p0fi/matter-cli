// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package hepafiltermonitoring implements the Matter HEPA Filter Monitoring cluster (0x0071).
package hepafiltermonitoring

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for HEPA Filter Monitoring.
	ID uint32 = 0x0071
	// Name is the CLI-friendly cluster name.
	Name = "hepa-filter-monitoring"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "HEPA Filter Monitoring"
)

// Attribute IDs.
const (
	AttrCondition              uint32 = 0x0000
	AttrDegradationDirection   uint32 = 0x0001
	AttrChangeIndication       uint32 = 0x0002
	AttrInPlaceIndicator       uint32 = 0x0003
	AttrLastChangedTime        uint32 = 0x0004
	AttrReplacementProductList uint32 = 0x0005
)

// Command IDs.
const (
	CmdResetCondition uint32 = 0x00
)

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrCondition, Name: "condition", DisplayName: "Condition", Type: "uint8", Readable: true, Optional: true},
			{ID: AttrDegradationDirection, Name: "degradation-direction", DisplayName: "DegradationDirection", Type: "enum8", Readable: true, Optional: true},
			{ID: AttrChangeIndication, Name: "change-indication", DisplayName: "ChangeIndication", Type: "enum8", Readable: true},
			{ID: AttrInPlaceIndicator, Name: "in-place-indicator", DisplayName: "InPlaceIndicator", Type: "bool", Readable: true, Optional: true},
			{ID: AttrLastChangedTime, Name: "last-changed-time", DisplayName: "LastChangedTime", Type: "uint32", Readable: true, Writable: true, Nullable: true, Optional: true},
			{ID: AttrReplacementProductList, Name: "replacement-product-list", DisplayName: "ReplacementProductList", Type: "list", Readable: true, Optional: true},
		},
		Commands: []clusters.CommandInfo{
			{ID: CmdResetCondition, Name: "reset-condition", DisplayName: "ResetCondition"},
		},
	})
}
