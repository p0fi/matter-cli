// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package thermostat implements the Matter Thermostat cluster (0x0201).
package thermostat

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Thermostat.
	ID uint32 = 0x0201
	// Name is the CLI-friendly cluster name.
	Name = "Thermostat"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Thermostat"
)

// Attribute IDs.
const (
	AttrLocalTemperature             uint32 = 0x0000
	AttrOutdoorTemperature           uint32 = 0x0001
	AttrOccupancy                    uint32 = 0x0002
	AttrAbsMinHeatSetpointLimit      uint32 = 0x0003
	AttrAbsMaxHeatSetpointLimit      uint32 = 0x0004
	AttrAbsMinCoolSetpointLimit      uint32 = 0x0005
	AttrAbsMaxCoolSetpointLimit      uint32 = 0x0006
	AttrPICoolingDemand              uint32 = 0x0007
	AttrPIHeatingDemand              uint32 = 0x0008
	AttrOccupiedCoolingSetpoint      uint32 = 0x0011
	AttrOccupiedHeatingSetpoint      uint32 = 0x0012
	AttrUnoccupiedCoolingSetpoint    uint32 = 0x0013
	AttrUnoccupiedHeatingSetpoint    uint32 = 0x0014
	AttrMinHeatSetpointLimit         uint32 = 0x0015
	AttrMaxHeatSetpointLimit         uint32 = 0x0016
	AttrMinCoolSetpointLimit         uint32 = 0x0017
	AttrMaxCoolSetpointLimit         uint32 = 0x0018
	AttrMinSetpointDeadBand          uint32 = 0x0019
	AttrControlSequenceOfOperation   uint32 = 0x001B
	AttrSystemMode                   uint32 = 0x001C
	AttrThermostatRunningMode        uint32 = 0x001E
	AttrStartOfWeek                  uint32 = 0x0020
	AttrNumberOfWeeklyTransitions    uint32 = 0x0021
	AttrNumberOfDailyTransitions     uint32 = 0x0022
	AttrTemperatureSetpointHold      uint32 = 0x0023
	AttrTemperatureSetpointHoldDuration uint32 = 0x0024
)

// Command IDs.
const (
	CmdSetpointRaiseLower     uint32 = 0x00
	CmdSetWeeklySchedule      uint32 = 0x01
	CmdGetWeeklySchedule      uint32 = 0x02
	CmdClearWeeklySchedule    uint32 = 0x03
)

// SetpointRaiseLowerRequest is the request payload for the SetpointRaiseLower command.
type SetpointRaiseLowerRequest struct {
	Mode   uint8 `tlv:"0,uint"`
	Amount int8  `tlv:"1,int"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrLocalTemperature, Name: "LocalTemperature", DisplayName: "LocalTemperature", Type: "int16", Readable: true, Nullable: true},
			{ID: AttrOutdoorTemperature, Name: "OutdoorTemperature", DisplayName: "OutdoorTemperature", Type: "int16", Readable: true, Nullable: true, Optional: true},
			{ID: AttrOccupancy, Name: "Occupancy", DisplayName: "Occupancy", Type: "bitmap8", Readable: true, Optional: true},
			{ID: AttrAbsMinHeatSetpointLimit, Name: "AbsMinHeatSetpointLimit", DisplayName: "AbsMinHeatSetpointLimit", Type: "int16", Readable: true, Optional: true},
			{ID: AttrAbsMaxHeatSetpointLimit, Name: "AbsMaxHeatSetpointLimit", DisplayName: "AbsMaxHeatSetpointLimit", Type: "int16", Readable: true, Optional: true},
			{ID: AttrAbsMinCoolSetpointLimit, Name: "AbsMinCoolSetpointLimit", DisplayName: "AbsMinCoolSetpointLimit", Type: "int16", Readable: true, Optional: true},
			{ID: AttrAbsMaxCoolSetpointLimit, Name: "AbsMaxCoolSetpointLimit", DisplayName: "AbsMaxCoolSetpointLimit", Type: "int16", Readable: true, Optional: true},
			{ID: AttrOccupiedCoolingSetpoint, Name: "OccupiedCoolingSetpoint", DisplayName: "OccupiedCoolingSetpoint", Type: "int16", Readable: true, Writable: true, Optional: true},
			{ID: AttrOccupiedHeatingSetpoint, Name: "OccupiedHeatingSetpoint", DisplayName: "OccupiedHeatingSetpoint", Type: "int16", Readable: true, Writable: true, Optional: true},
			{ID: AttrMinHeatSetpointLimit, Name: "MinHeatSetpointLimit", DisplayName: "MinHeatSetpointLimit", Type: "int16", Readable: true, Writable: true, Optional: true},
			{ID: AttrMaxHeatSetpointLimit, Name: "MaxHeatSetpointLimit", DisplayName: "MaxHeatSetpointLimit", Type: "int16", Readable: true, Writable: true, Optional: true},
			{ID: AttrMinCoolSetpointLimit, Name: "MinCoolSetpointLimit", DisplayName: "MinCoolSetpointLimit", Type: "int16", Readable: true, Writable: true, Optional: true},
			{ID: AttrMaxCoolSetpointLimit, Name: "MaxCoolSetpointLimit", DisplayName: "MaxCoolSetpointLimit", Type: "int16", Readable: true, Writable: true, Optional: true},
			{ID: AttrMinSetpointDeadBand, Name: "MinSetpointDeadBand", DisplayName: "MinSetpointDeadBand", Type: "int8", Readable: true, Writable: true, Optional: true},
			{ID: AttrControlSequenceOfOperation, Name: "ControlSequenceOfOperation", DisplayName: "ControlSequenceOfOperation", Type: "enum8", Readable: true, Writable: true},
			{ID: AttrSystemMode, Name: "SystemMode", DisplayName: "SystemMode", Type: "enum8", Readable: true, Writable: true},
			{ID: AttrThermostatRunningMode, Name: "ThermostatRunningMode", DisplayName: "ThermostatRunningMode", Type: "enum8", Readable: true, Optional: true},
		},
		Commands: []clusters.CommandInfo{
			{ID: CmdSetpointRaiseLower, Name: "SetpointRaiseLower", DisplayName: "SetpointRaiseLower", HasRequest: true},
			{ID: CmdSetWeeklySchedule, Name: "SetWeeklySchedule", DisplayName: "SetWeeklySchedule", HasRequest: true},
			{ID: CmdGetWeeklySchedule, Name: "GetWeeklySchedule", DisplayName: "GetWeeklySchedule", HasRequest: true, HasResponse: true},
			{ID: CmdClearWeeklySchedule, Name: "ClearWeeklySchedule", DisplayName: "ClearWeeklySchedule"},
		},
	})
}
