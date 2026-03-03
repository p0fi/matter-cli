// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package colorcontrol implements the Matter Color Control cluster (0x0300).
package colorcontrol

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Color Control.
	ID uint32 = 0x0300
	// Name is the CLI-friendly cluster name.
	Name = "ColorControl"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Color Control"
)

// Attribute IDs.
const (
	AttrCurrentHue                uint32 = 0x0000
	AttrCurrentSaturation         uint32 = 0x0001
	AttrRemainingTime             uint32 = 0x0002
	AttrCurrentX                  uint32 = 0x0003
	AttrCurrentY                  uint32 = 0x0004
	AttrDriftCompensation         uint32 = 0x0005
	AttrCompensationText          uint32 = 0x0006
	AttrColorTemperatureMireds    uint32 = 0x0007
	AttrColorMode                 uint32 = 0x0008
	AttrOptions                   uint32 = 0x000F
	AttrNumberOfPrimaries         uint32 = 0x0010
	AttrEnhancedCurrentHue        uint32 = 0x4000
	AttrEnhancedColorMode         uint32 = 0x4001
	AttrColorLoopActive           uint32 = 0x4002
	AttrColorLoopDirection        uint32 = 0x4003
	AttrColorLoopTime             uint32 = 0x4004
	AttrColorLoopStartHue         uint32 = 0x4005
	AttrColorCapabilities         uint32 = 0x400A
	AttrColorTempPhysicalMinMireds uint32 = 0x400B
	AttrColorTempPhysicalMaxMireds uint32 = 0x400C
	AttrCoupleColorTempToLevel    uint32 = 0x400D
	AttrStartUpColorTempMireds    uint32 = 0x4010
)

// Command IDs.
const (
	CmdMoveToHue                 uint32 = 0x00
	CmdMoveHue                   uint32 = 0x01
	CmdStepHue                   uint32 = 0x02
	CmdMoveToSaturation          uint32 = 0x03
	CmdMoveSaturation            uint32 = 0x04
	CmdStepSaturation            uint32 = 0x05
	CmdMoveToHueAndSaturation    uint32 = 0x06
	CmdMoveToColor               uint32 = 0x07
	CmdMoveColor                 uint32 = 0x08
	CmdStepColor                 uint32 = 0x09
	CmdMoveToColorTemperature    uint32 = 0x0A
	CmdEnhancedMoveToHue         uint32 = 0x40
	CmdEnhancedMoveHue           uint32 = 0x41
	CmdEnhancedStepHue           uint32 = 0x42
	CmdEnhancedMoveToHueAndSat   uint32 = 0x43
	CmdColorLoopSet              uint32 = 0x44
	CmdStopMoveStep              uint32 = 0x47
	CmdMoveColorTemperature      uint32 = 0x4B
	CmdStepColorTemperature      uint32 = 0x4C
)

// MoveToHueRequest is the request payload for the MoveToHue command.
type MoveToHueRequest struct {
	Hue             uint8  `tlv:"0,uint"`
	Direction       uint8  `tlv:"1,uint"`
	TransitionTime  uint16 `tlv:"2,uint"`
	OptionsMask     uint8  `tlv:"3,uint"`
	OptionsOverride uint8  `tlv:"4,uint"`
}

// MoveToSaturationRequest is the request payload for the MoveToSaturation command.
type MoveToSaturationRequest struct {
	Saturation      uint8  `tlv:"0,uint"`
	TransitionTime  uint16 `tlv:"1,uint"`
	OptionsMask     uint8  `tlv:"2,uint"`
	OptionsOverride uint8  `tlv:"3,uint"`
}

// MoveToColorRequest is the request payload for the MoveToColor command.
type MoveToColorRequest struct {
	ColorX          uint16 `tlv:"0,uint"`
	ColorY          uint16 `tlv:"1,uint"`
	TransitionTime  uint16 `tlv:"2,uint"`
	OptionsMask     uint8  `tlv:"3,uint"`
	OptionsOverride uint8  `tlv:"4,uint"`
}

// MoveToColorTemperatureRequest is the request payload for the MoveToColorTemperature command.
type MoveToColorTemperatureRequest struct {
	ColorTemperatureMireds uint16 `tlv:"0,uint"`
	TransitionTime         uint16 `tlv:"1,uint"`
	OptionsMask            uint8  `tlv:"2,uint"`
	OptionsOverride        uint8  `tlv:"3,uint"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrCurrentHue, Name: "CurrentHue", DisplayName: "CurrentHue", Type: "uint8", Readable: true},
			{ID: AttrCurrentSaturation, Name: "CurrentSaturation", DisplayName: "CurrentSaturation", Type: "uint8", Readable: true},
			{ID: AttrRemainingTime, Name: "RemainingTime", DisplayName: "RemainingTime", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrCurrentX, Name: "CurrentX", DisplayName: "CurrentX", Type: "uint16", Readable: true},
			{ID: AttrCurrentY, Name: "CurrentY", DisplayName: "CurrentY", Type: "uint16", Readable: true},
			{ID: AttrColorTemperatureMireds, Name: "ColorTemperatureMireds", DisplayName: "ColorTemperatureMireds", Type: "uint16", Readable: true},
			{ID: AttrColorMode, Name: "ColorMode", DisplayName: "ColorMode", Type: "enum8", Readable: true},
			{ID: AttrOptions, Name: "Options", DisplayName: "Options", Type: "bitmap8", Readable: true, Writable: true},
			{ID: AttrEnhancedCurrentHue, Name: "EnhancedCurrentHue", DisplayName: "EnhancedCurrentHue", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrEnhancedColorMode, Name: "EnhancedColorMode", DisplayName: "EnhancedColorMode", Type: "enum8", Readable: true},
			{ID: AttrColorLoopActive, Name: "ColorLoopActive", DisplayName: "ColorLoopActive", Type: "uint8", Readable: true, Optional: true},
			{ID: AttrColorLoopDirection, Name: "ColorLoopDirection", DisplayName: "ColorLoopDirection", Type: "uint8", Readable: true, Optional: true},
			{ID: AttrColorLoopTime, Name: "ColorLoopTime", DisplayName: "ColorLoopTime", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrColorCapabilities, Name: "ColorCapabilities", DisplayName: "ColorCapabilities", Type: "bitmap16", Readable: true},
			{ID: AttrColorTempPhysicalMinMireds, Name: "ColorTempPhysicalMinMireds", DisplayName: "ColorTempPhysicalMinMireds", Type: "uint16", Readable: true},
			{ID: AttrColorTempPhysicalMaxMireds, Name: "ColorTempPhysicalMaxMireds", DisplayName: "ColorTempPhysicalMaxMireds", Type: "uint16", Readable: true},
			{ID: AttrStartUpColorTempMireds, Name: "StartUpColorTemperatureMireds", DisplayName: "StartUpColorTemperatureMireds", Type: "uint16", Readable: true, Writable: true, Nullable: true, Optional: true},
		},
		Commands: []clusters.CommandInfo{
			{ID: CmdMoveToHue, Name: "MoveToHue", DisplayName: "MoveToHue", HasRequest: true},
			{ID: CmdMoveHue, Name: "MoveHue", DisplayName: "MoveHue", HasRequest: true},
			{ID: CmdStepHue, Name: "StepHue", DisplayName: "StepHue", HasRequest: true},
			{ID: CmdMoveToSaturation, Name: "MoveToSaturation", DisplayName: "MoveToSaturation", HasRequest: true},
			{ID: CmdMoveSaturation, Name: "MoveSaturation", DisplayName: "MoveSaturation", HasRequest: true},
			{ID: CmdStepSaturation, Name: "StepSaturation", DisplayName: "StepSaturation", HasRequest: true},
			{ID: CmdMoveToHueAndSaturation, Name: "MoveToHueAndSaturation", DisplayName: "MoveToHueAndSaturation", HasRequest: true},
			{ID: CmdMoveToColor, Name: "MoveToColor", DisplayName: "MoveToColor", HasRequest: true},
			{ID: CmdMoveColor, Name: "MoveColor", DisplayName: "MoveColor", HasRequest: true},
			{ID: CmdStepColor, Name: "StepColor", DisplayName: "StepColor", HasRequest: true},
			{ID: CmdMoveToColorTemperature, Name: "MoveToColorTemperature", DisplayName: "MoveToColorTemperature", HasRequest: true},
			{ID: CmdStopMoveStep, Name: "StopMoveStep", DisplayName: "StopMoveStep", HasRequest: true},
			{ID: CmdMoveColorTemperature, Name: "MoveColorTemperature", DisplayName: "MoveColorTemperature", HasRequest: true},
			{ID: CmdStepColorTemperature, Name: "StepColorTemperature", DisplayName: "StepColorTemperature", HasRequest: true},
		},
	})
}
