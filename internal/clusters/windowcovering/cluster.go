// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package windowcovering implements the Matter Window Covering cluster (0x0102).
package windowcovering

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Window Covering.
	ID uint32 = 0x0102
	// Name is the CLI-friendly cluster name.
	Name = "window-covering"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Window Covering"
)

// Attribute IDs.
const (
	AttrType                            uint32 = 0x0000
	AttrPhysicalClosedLimitLift         uint32 = 0x0001
	AttrPhysicalClosedLimitTilt         uint32 = 0x0002
	AttrCurrentPositionLift             uint32 = 0x0003
	AttrCurrentPositionTilt             uint32 = 0x0004
	AttrNumberOfActuationsLift          uint32 = 0x0005
	AttrNumberOfActuationsTilt          uint32 = 0x0006
	AttrConfigStatus                    uint32 = 0x0007
	AttrCurrentPositionLiftPercentage   uint32 = 0x0008
	AttrCurrentPositionTiltPercentage   uint32 = 0x0009
	AttrOperationalStatus               uint32 = 0x000A
	AttrTargetPositionLiftPercent100ths uint32 = 0x000B
	AttrTargetPositionTiltPercent100ths uint32 = 0x000C
	AttrEndProductType                  uint32 = 0x000D
	AttrCurrentPositionLiftPercent100ths uint32 = 0x000E
	AttrCurrentPositionTiltPercent100ths uint32 = 0x000F
	AttrInstalledOpenLimitLift          uint32 = 0x0010
	AttrInstalledClosedLimitLift        uint32 = 0x0011
	AttrInstalledOpenLimitTilt          uint32 = 0x0012
	AttrInstalledClosedLimitTilt        uint32 = 0x0013
	AttrMode                            uint32 = 0x0017
	AttrSafetyStatus                    uint32 = 0x001A
)

// Command IDs.
const (
	CmdUpOrOpen                uint32 = 0x00
	CmdDownOrClose             uint32 = 0x01
	CmdStopMotion              uint32 = 0x02
	CmdGoToLiftValue           uint32 = 0x04
	CmdGoToLiftPercentage      uint32 = 0x05
	CmdGoToTiltValue           uint32 = 0x07
	CmdGoToTiltPercentage      uint32 = 0x08
)

// GoToLiftPercentageRequest is the request payload for the GoToLiftPercentage command.
type GoToLiftPercentageRequest struct {
	LiftPercent100thsValue uint16 `tlv:"0,uint"`
}

// GoToTiltPercentageRequest is the request payload for the GoToTiltPercentage command.
type GoToTiltPercentageRequest struct {
	TiltPercent100thsValue uint16 `tlv:"0,uint"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrType, Name: "type", DisplayName: "Type", Type: "enum8", Readable: true},
			{ID: AttrConfigStatus, Name: "config-status", DisplayName: "ConfigStatus", Type: "bitmap8", Readable: true},
			{ID: AttrCurrentPositionLiftPercentage, Name: "current-position-lift-percentage", DisplayName: "CurrentPositionLiftPercentage", Type: "uint8", Readable: true, Nullable: true, Optional: true},
			{ID: AttrCurrentPositionTiltPercentage, Name: "current-position-tilt-percentage", DisplayName: "CurrentPositionTiltPercentage", Type: "uint8", Readable: true, Nullable: true, Optional: true},
			{ID: AttrOperationalStatus, Name: "operational-status", DisplayName: "OperationalStatus", Type: "bitmap8", Readable: true},
			{ID: AttrTargetPositionLiftPercent100ths, Name: "target-position-lift-percent-100ths", DisplayName: "TargetPositionLiftPercent100ths", Type: "uint16", Readable: true, Nullable: true, Optional: true},
			{ID: AttrTargetPositionTiltPercent100ths, Name: "target-position-tilt-percent-100ths", DisplayName: "TargetPositionTiltPercent100ths", Type: "uint16", Readable: true, Nullable: true, Optional: true},
			{ID: AttrEndProductType, Name: "end-product-type", DisplayName: "EndProductType", Type: "enum8", Readable: true},
			{ID: AttrCurrentPositionLiftPercent100ths, Name: "current-position-lift-percent-100ths", DisplayName: "CurrentPositionLiftPercent100ths", Type: "uint16", Readable: true, Nullable: true, Optional: true},
			{ID: AttrCurrentPositionTiltPercent100ths, Name: "current-position-tilt-percent-100ths", DisplayName: "CurrentPositionTiltPercent100ths", Type: "uint16", Readable: true, Nullable: true, Optional: true},
			{ID: AttrInstalledOpenLimitLift, Name: "installed-open-limit-lift", DisplayName: "InstalledOpenLimitLift", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrInstalledClosedLimitLift, Name: "installed-closed-limit-lift", DisplayName: "InstalledClosedLimitLift", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrInstalledOpenLimitTilt, Name: "installed-open-limit-tilt", DisplayName: "InstalledOpenLimitTilt", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrInstalledClosedLimitTilt, Name: "installed-closed-limit-tilt", DisplayName: "InstalledClosedLimitTilt", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrMode, Name: "mode", DisplayName: "Mode", Type: "bitmap8", Readable: true, Writable: true},
			{ID: AttrSafetyStatus, Name: "safety-status", DisplayName: "SafetyStatus", Type: "bitmap16", Readable: true, Optional: true},
		},
		Commands: []clusters.CommandInfo{
			{ID: CmdUpOrOpen, Name: "up-or-open", DisplayName: "UpOrOpen"},
			{ID: CmdDownOrClose, Name: "down-or-close", DisplayName: "DownOrClose"},
			{ID: CmdStopMotion, Name: "stop-motion", DisplayName: "StopMotion"},
			{ID: CmdGoToLiftPercentage, Name: "go-to-lift-percentage", DisplayName: "GoToLiftPercentage", HasRequest: true},
			{ID: CmdGoToTiltPercentage, Name: "go-to-tilt-percentage", DisplayName: "GoToTiltPercentage", HasRequest: true},
		},
	})
}
