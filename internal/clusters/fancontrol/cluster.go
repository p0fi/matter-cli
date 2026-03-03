// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package fancontrol implements the Matter Fan Control cluster (0x0202).
package fancontrol

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Fan Control.
	ID uint32 = 0x0202
	// Name is the CLI-friendly cluster name.
	Name = "FanControl"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Fan Control"
)

// Attribute IDs.
const (
	AttrFanMode          uint32 = 0x0000
	AttrFanModeSequence  uint32 = 0x0001
	AttrPercentSetting   uint32 = 0x0002
	AttrPercentCurrent   uint32 = 0x0003
	AttrSpeedMax         uint32 = 0x0004
	AttrSpeedSetting     uint32 = 0x0005
	AttrSpeedCurrent     uint32 = 0x0006
	AttrRockSupport      uint32 = 0x0007
	AttrRockSetting      uint32 = 0x0008
	AttrWindSupport      uint32 = 0x0009
	AttrWindSetting      uint32 = 0x000A
	AttrAirflowDirection uint32 = 0x000B
)

// Command IDs.
const (
	CmdStep uint32 = 0x00
)

// StepRequest is the request payload for the Step command.
type StepRequest struct {
	Direction uint8 `tlv:"0,uint"`
	Wrap      *bool `tlv:"1,bool"`
	LowestOff *bool `tlv:"2,bool"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrFanMode, Name: "FanMode", DisplayName: "FanMode", Type: "enum8", Readable: true, Writable: true},
			{ID: AttrFanModeSequence, Name: "FanModeSequence", DisplayName: "FanModeSequence", Type: "enum8", Readable: true},
			{ID: AttrPercentSetting, Name: "PercentSetting", DisplayName: "PercentSetting", Type: "uint8", Readable: true, Writable: true, Nullable: true},
			{ID: AttrPercentCurrent, Name: "PercentCurrent", DisplayName: "PercentCurrent", Type: "uint8", Readable: true},
			{ID: AttrSpeedMax, Name: "SpeedMax", DisplayName: "SpeedMax", Type: "uint8", Readable: true, Optional: true},
			{ID: AttrSpeedSetting, Name: "SpeedSetting", DisplayName: "SpeedSetting", Type: "uint8", Readable: true, Writable: true, Nullable: true, Optional: true},
			{ID: AttrSpeedCurrent, Name: "SpeedCurrent", DisplayName: "SpeedCurrent", Type: "uint8", Readable: true, Optional: true},
			{ID: AttrRockSupport, Name: "RockSupport", DisplayName: "RockSupport", Type: "bitmap8", Readable: true, Optional: true},
			{ID: AttrRockSetting, Name: "RockSetting", DisplayName: "RockSetting", Type: "bitmap8", Readable: true, Writable: true, Optional: true},
			{ID: AttrWindSupport, Name: "WindSupport", DisplayName: "WindSupport", Type: "bitmap8", Readable: true, Optional: true},
			{ID: AttrWindSetting, Name: "WindSetting", DisplayName: "WindSetting", Type: "bitmap8", Readable: true, Writable: true, Optional: true},
			{ID: AttrAirflowDirection, Name: "AirflowDirection", DisplayName: "AirflowDirection", Type: "enum8", Readable: true, Writable: true, Optional: true},
		},
		Commands: []clusters.CommandInfo{
			{ID: CmdStep, Name: "Step", DisplayName: "Step", HasRequest: true},
		},
	})
}
