// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package levelcontrol implements the Matter Level Control cluster (0x0008).
package levelcontrol

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Level Control.
	ID uint32 = 0x0008
	// Name is the CLI-friendly cluster name.
	Name = "level-control"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Level Control"
)

// Attribute IDs.
const (
	AttrCurrentLevel        uint32 = 0x0000
	AttrRemainingTime       uint32 = 0x0001
	AttrMinLevel            uint32 = 0x0002
	AttrMaxLevel            uint32 = 0x0003
	AttrCurrentFrequency    uint32 = 0x0004
	AttrMinFrequency        uint32 = 0x0005
	AttrMaxFrequency        uint32 = 0x0006
	AttrOnOffTransitionTime uint32 = 0x0010
	AttrOnLevel             uint32 = 0x0011
	AttrOnTransitionTime    uint32 = 0x0012
	AttrOffTransitionTime   uint32 = 0x0013
	AttrDefaultMoveRate     uint32 = 0x0014
	AttrOptions             uint32 = 0x000F
	AttrStartUpCurrentLevel uint32 = 0x4000
)

// Command IDs.
const (
	CmdMoveToLevel          uint32 = 0x00
	CmdMove                 uint32 = 0x01
	CmdStep                 uint32 = 0x02
	CmdStop                 uint32 = 0x03
	CmdMoveToLevelWithOnOff uint32 = 0x04
	CmdMoveWithOnOff        uint32 = 0x05
	CmdStepWithOnOff        uint32 = 0x06
	CmdStopWithOnOff        uint32 = 0x07
)

// MoveToLevelRequest is the request payload for the MoveToLevel command.
type MoveToLevelRequest struct {
	Level          uint8  `tlv:"0,uint"`
	TransitionTime uint16 `tlv:"1,uint"`
	OptionsMask    uint8  `tlv:"2,uint"`
	OptionsOverride uint8 `tlv:"3,uint"`
}

// MoveRequest is the request payload for the Move command.
type MoveRequest struct {
	MoveMode        uint8 `tlv:"0,uint"`
	Rate            uint8 `tlv:"1,uint"`
	OptionsMask     uint8 `tlv:"2,uint"`
	OptionsOverride uint8 `tlv:"3,uint"`
}

// StepRequest is the request payload for the Step command.
type StepRequest struct {
	StepMode        uint8  `tlv:"0,uint"`
	StepSize        uint8  `tlv:"1,uint"`
	TransitionTime  uint16 `tlv:"2,uint"`
	OptionsMask     uint8  `tlv:"3,uint"`
	OptionsOverride uint8  `tlv:"4,uint"`
}

// StopRequest is the request payload for the Stop command.
type StopRequest struct {
	OptionsMask     uint8 `tlv:"0,uint"`
	OptionsOverride uint8 `tlv:"1,uint"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrCurrentLevel, Name: "current-level", DisplayName: "CurrentLevel", Type: "uint8", Readable: true, Nullable: true},
			{ID: AttrRemainingTime, Name: "remaining-time", DisplayName: "RemainingTime", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrMinLevel, Name: "min-level", DisplayName: "MinLevel", Type: "uint8", Readable: true, Optional: true},
			{ID: AttrMaxLevel, Name: "max-level", DisplayName: "MaxLevel", Type: "uint8", Readable: true, Optional: true},
			{ID: AttrCurrentFrequency, Name: "current-frequency", DisplayName: "CurrentFrequency", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrMinFrequency, Name: "min-frequency", DisplayName: "MinFrequency", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrMaxFrequency, Name: "max-frequency", DisplayName: "MaxFrequency", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrOptions, Name: "options", DisplayName: "Options", Type: "bitmap8", Readable: true, Writable: true},
			{ID: AttrOnOffTransitionTime, Name: "on-off-transition-time", DisplayName: "OnOffTransitionTime", Type: "uint16", Readable: true, Writable: true, Optional: true},
			{ID: AttrOnLevel, Name: "on-level", DisplayName: "OnLevel", Type: "uint8", Readable: true, Writable: true, Nullable: true},
			{ID: AttrOnTransitionTime, Name: "on-transition-time", DisplayName: "OnTransitionTime", Type: "uint16", Readable: true, Writable: true, Optional: true, Nullable: true},
			{ID: AttrOffTransitionTime, Name: "off-transition-time", DisplayName: "OffTransitionTime", Type: "uint16", Readable: true, Writable: true, Optional: true, Nullable: true},
			{ID: AttrDefaultMoveRate, Name: "default-move-rate", DisplayName: "DefaultMoveRate", Type: "uint8", Readable: true, Writable: true, Optional: true, Nullable: true},
			{ID: AttrStartUpCurrentLevel, Name: "start-up-current-level", DisplayName: "StartUpCurrentLevel", Type: "uint8", Readable: true, Writable: true, Nullable: true},
		},
		Commands: []clusters.CommandInfo{
			{ID: CmdMoveToLevel, Name: "move-to-level", DisplayName: "MoveToLevel", HasRequest: true, RequestFields: []clusters.CommandFieldInfo{
				{ID: 0, Name: "level", DisplayName: "Level", Type: "uint8"},
				{ID: 1, Name: "transition-time", DisplayName: "TransitionTime", Type: "uint16", Nullable: true},
				{ID: 2, Name: "options-mask", DisplayName: "OptionsMask", Type: "bitmap8", Optional: true},
				{ID: 3, Name: "options-override", DisplayName: "OptionsOverride", Type: "bitmap8", Optional: true},
			}},
			{ID: CmdMove, Name: "move", DisplayName: "Move", HasRequest: true, RequestFields: []clusters.CommandFieldInfo{
				{ID: 0, Name: "move-mode", DisplayName: "MoveMode", Type: "enum8"},
				{ID: 1, Name: "rate", DisplayName: "Rate", Type: "uint8", Nullable: true},
				{ID: 2, Name: "options-mask", DisplayName: "OptionsMask", Type: "bitmap8", Optional: true},
				{ID: 3, Name: "options-override", DisplayName: "OptionsOverride", Type: "bitmap8", Optional: true},
			}},
			{ID: CmdStep, Name: "step", DisplayName: "Step", HasRequest: true, RequestFields: []clusters.CommandFieldInfo{
				{ID: 0, Name: "step-mode", DisplayName: "StepMode", Type: "enum8"},
				{ID: 1, Name: "step-size", DisplayName: "StepSize", Type: "uint8"},
				{ID: 2, Name: "transition-time", DisplayName: "TransitionTime", Type: "uint16", Nullable: true},
				{ID: 3, Name: "options-mask", DisplayName: "OptionsMask", Type: "bitmap8", Optional: true},
				{ID: 4, Name: "options-override", DisplayName: "OptionsOverride", Type: "bitmap8", Optional: true},
			}},
			{ID: CmdStop, Name: "stop", DisplayName: "Stop", HasRequest: true, RequestFields: []clusters.CommandFieldInfo{
				{ID: 0, Name: "options-mask", DisplayName: "OptionsMask", Type: "bitmap8", Optional: true},
				{ID: 1, Name: "options-override", DisplayName: "OptionsOverride", Type: "bitmap8", Optional: true},
			}},
			{ID: CmdMoveToLevelWithOnOff, Name: "move-to-level-with-on-off", DisplayName: "MoveToLevelWithOnOff", HasRequest: true, RequestFields: []clusters.CommandFieldInfo{
				{ID: 0, Name: "level", DisplayName: "Level", Type: "uint8"},
				{ID: 1, Name: "transition-time", DisplayName: "TransitionTime", Type: "uint16", Nullable: true},
				{ID: 2, Name: "options-mask", DisplayName: "OptionsMask", Type: "bitmap8", Optional: true},
				{ID: 3, Name: "options-override", DisplayName: "OptionsOverride", Type: "bitmap8", Optional: true},
			}},
			{ID: CmdMoveWithOnOff, Name: "move-with-on-off", DisplayName: "MoveWithOnOff", HasRequest: true, RequestFields: []clusters.CommandFieldInfo{
				{ID: 0, Name: "move-mode", DisplayName: "MoveMode", Type: "enum8"},
				{ID: 1, Name: "rate", DisplayName: "Rate", Type: "uint8", Nullable: true},
				{ID: 2, Name: "options-mask", DisplayName: "OptionsMask", Type: "bitmap8", Optional: true},
				{ID: 3, Name: "options-override", DisplayName: "OptionsOverride", Type: "bitmap8", Optional: true},
			}},
			{ID: CmdStepWithOnOff, Name: "step-with-on-off", DisplayName: "StepWithOnOff", HasRequest: true, RequestFields: []clusters.CommandFieldInfo{
				{ID: 0, Name: "step-mode", DisplayName: "StepMode", Type: "enum8"},
				{ID: 1, Name: "step-size", DisplayName: "StepSize", Type: "uint8"},
				{ID: 2, Name: "transition-time", DisplayName: "TransitionTime", Type: "uint16", Nullable: true},
				{ID: 3, Name: "options-mask", DisplayName: "OptionsMask", Type: "bitmap8", Optional: true},
				{ID: 4, Name: "options-override", DisplayName: "OptionsOverride", Type: "bitmap8", Optional: true},
			}},
			{ID: CmdStopWithOnOff, Name: "stop-with-on-off", DisplayName: "StopWithOnOff", HasRequest: true, RequestFields: []clusters.CommandFieldInfo{
				{ID: 0, Name: "options-mask", DisplayName: "OptionsMask", Type: "bitmap8", Optional: true},
				{ID: 1, Name: "options-override", DisplayName: "OptionsOverride", Type: "bitmap8", Optional: true},
			}},
		},
	})
}
