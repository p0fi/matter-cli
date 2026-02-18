// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package onoff implements the Matter On/Off cluster (0x0006).
package onoff

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for On/Off.
	ID uint32 = 0x0006
	// Name is the CLI-friendly cluster name.
	Name = "on-off"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "On/Off"
)

// Attribute IDs.
const (
	AttrOnOff              uint32 = 0x0000
	AttrGlobalSceneControl uint32 = 0x4000
	AttrOnTime             uint32 = 0x4001
	AttrOffWaitTime        uint32 = 0x4002
	AttrStartUpOnOff       uint32 = 0x4003
)

// Command IDs.
const (
	CmdOff                     uint32 = 0x00
	CmdOn                      uint32 = 0x01
	CmdToggle                  uint32 = 0x02
	CmdOffWithEffect           uint32 = 0x40
	CmdOnWithRecallGlobalScene uint32 = 0x41
	CmdOnWithTimedOff          uint32 = 0x42
)

// OffWithEffectRequest is the request payload for the OffWithEffect command.
type OffWithEffectRequest struct {
	EffectIdentifier uint8  `tlv:"0,uint"`
	EffectVariant    uint8  `tlv:"1,uint"`
}

// OnWithTimedOffRequest is the request payload for the OnWithTimedOff command.
type OnWithTimedOffRequest struct {
	OnOffControl uint8  `tlv:"0,uint"`
	OnTime       uint16 `tlv:"1,uint"`
	OffWaitTime  uint16 `tlv:"2,uint"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrOnOff, Name: "on-off", DisplayName: "OnOff", Type: "bool", Readable: true},
			{ID: AttrGlobalSceneControl, Name: "global-scene-control", DisplayName: "GlobalSceneControl", Type: "bool", Readable: true},
			{ID: AttrOnTime, Name: "on-time", DisplayName: "OnTime", Type: "uint16", Readable: true, Writable: true},
			{ID: AttrOffWaitTime, Name: "off-wait-time", DisplayName: "OffWaitTime", Type: "uint16", Readable: true, Writable: true},
			{ID: AttrStartUpOnOff, Name: "start-up-on-off", DisplayName: "StartUpOnOff", Type: "enum8", Readable: true, Writable: true, Nullable: true},
		},
		Commands: []clusters.CommandInfo{
			{ID: CmdOff, Name: "off", DisplayName: "Off"},
			{ID: CmdOn, Name: "on", DisplayName: "On"},
			{ID: CmdToggle, Name: "toggle", DisplayName: "Toggle"},
			{ID: CmdOffWithEffect, Name: "off-with-effect", DisplayName: "OffWithEffect", HasRequest: true, RequestFields: []clusters.CommandFieldInfo{
				{ID: 0, Name: "effect-identifier", DisplayName: "EffectIdentifier", Type: "enum8"},
				{ID: 1, Name: "effect-variant", DisplayName: "EffectVariant", Type: "enum8"},
			}},
			{ID: CmdOnWithRecallGlobalScene, Name: "on-with-recall-global-scene", DisplayName: "OnWithRecallGlobalScene"},
			{ID: CmdOnWithTimedOff, Name: "on-with-timed-off", DisplayName: "OnWithTimedOff", HasRequest: true, RequestFields: []clusters.CommandFieldInfo{
				{ID: 0, Name: "on-off-control", DisplayName: "OnOffControl", Type: "bitmap8"},
				{ID: 1, Name: "on-time", DisplayName: "OnTime", Type: "uint16"},
				{ID: 2, Name: "off-wait-time", DisplayName: "OffWaitTime", Type: "uint16"},
			}},
		},
	})
}
