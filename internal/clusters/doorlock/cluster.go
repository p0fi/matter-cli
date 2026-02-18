// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package doorlock implements the Matter Door Lock cluster (0x0101).
package doorlock

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Door Lock.
	ID uint32 = 0x0101
	// Name is the CLI-friendly cluster name.
	Name = "door-lock"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Door Lock"
)

// Attribute IDs.
const (
	AttrLockState               uint32 = 0x0000
	AttrLockType                uint32 = 0x0001
	AttrActuatorEnabled         uint32 = 0x0002
	AttrDoorState               uint32 = 0x0003
	AttrNumberOfTotalUsersSupported uint32 = 0x0011
	AttrNumberOfPINUsersSupported   uint32 = 0x0012
	AttrNumberOfRFIDUsersSupported  uint32 = 0x0013
	AttrMaxPINCodeLength        uint32 = 0x0017
	AttrMinPINCodeLength        uint32 = 0x0018
	AttrMaxRFIDCodeLength       uint32 = 0x0019
	AttrMinRFIDCodeLength       uint32 = 0x001A
	AttrLanguage                uint32 = 0x0021
	AttrAutoRelockTime          uint32 = 0x0023
	AttrSoundVolume             uint32 = 0x0024
	AttrOperatingMode           uint32 = 0x0025
	AttrSupportedOperatingModes uint32 = 0x0026
	AttrEnableOneTouchLocking   uint32 = 0x0029
	AttrEnablePrivacyModeButton uint32 = 0x002B
	AttrWrongCodeEntryLimit     uint32 = 0x0030
	AttrUserCodeTemporaryDisableTime uint32 = 0x0031
	AttrRequirePINForRemoteOperation uint32 = 0x0033
)

// Command IDs.
const (
	CmdLockDoor            uint32 = 0x00
	CmdUnlockDoor          uint32 = 0x01
	CmdUnlockWithTimeout   uint32 = 0x03
	CmdSetUser             uint32 = 0x1A
	CmdGetUser             uint32 = 0x1B
	CmdClearUser           uint32 = 0x1D
	CmdSetCredential       uint32 = 0x22
	CmdGetCredentialStatus uint32 = 0x24
	CmdClearCredential     uint32 = 0x26
)

// LockDoorRequest is the request payload for the LockDoor command.
type LockDoorRequest struct {
	PINCode []byte `tlv:"0,octets,optional"`
}

// UnlockDoorRequest is the request payload for the UnlockDoor command.
type UnlockDoorRequest struct {
	PINCode []byte `tlv:"0,octets,optional"`
}

// UnlockWithTimeoutRequest is the request payload for the UnlockWithTimeout command.
type UnlockWithTimeoutRequest struct {
	Timeout uint16 `tlv:"0,uint"`
	PINCode []byte `tlv:"1,octets,optional"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrLockState, Name: "lock-state", DisplayName: "LockState", Type: "enum8", Readable: true, Nullable: true},
			{ID: AttrLockType, Name: "lock-type", DisplayName: "LockType", Type: "enum8", Readable: true},
			{ID: AttrActuatorEnabled, Name: "actuator-enabled", DisplayName: "ActuatorEnabled", Type: "bool", Readable: true},
			{ID: AttrDoorState, Name: "door-state", DisplayName: "DoorState", Type: "enum8", Readable: true, Nullable: true, Optional: true},
			{ID: AttrNumberOfTotalUsersSupported, Name: "number-of-total-users-supported", DisplayName: "NumberOfTotalUsersSupported", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrNumberOfPINUsersSupported, Name: "number-of-pin-users-supported", DisplayName: "NumberOfPINUsersSupported", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrNumberOfRFIDUsersSupported, Name: "number-of-rfid-users-supported", DisplayName: "NumberOfRFIDUsersSupported", Type: "uint16", Readable: true, Optional: true},
			{ID: AttrMaxPINCodeLength, Name: "max-pin-code-length", DisplayName: "MaxPINCodeLength", Type: "uint8", Readable: true, Optional: true},
			{ID: AttrMinPINCodeLength, Name: "min-pin-code-length", DisplayName: "MinPINCodeLength", Type: "uint8", Readable: true, Optional: true},
			{ID: AttrLanguage, Name: "language", DisplayName: "Language", Type: "string", Readable: true, Writable: true, Optional: true},
			{ID: AttrAutoRelockTime, Name: "auto-relock-time", DisplayName: "AutoRelockTime", Type: "uint32", Readable: true, Writable: true, Optional: true},
			{ID: AttrSoundVolume, Name: "sound-volume", DisplayName: "SoundVolume", Type: "uint8", Readable: true, Writable: true, Optional: true},
			{ID: AttrOperatingMode, Name: "operating-mode", DisplayName: "OperatingMode", Type: "enum8", Readable: true, Writable: true},
			{ID: AttrSupportedOperatingModes, Name: "supported-operating-modes", DisplayName: "SupportedOperatingModes", Type: "bitmap16", Readable: true},
			{ID: AttrEnableOneTouchLocking, Name: "enable-one-touch-locking", DisplayName: "EnableOneTouchLocking", Type: "bool", Readable: true, Writable: true, Optional: true},
			{ID: AttrRequirePINForRemoteOperation, Name: "require-pin-for-remote-operation", DisplayName: "RequirePINForRemoteOperation", Type: "bool", Readable: true, Writable: true, Optional: true},
		},
		Commands: []clusters.CommandInfo{
			{ID: CmdLockDoor, Name: "lock-door", DisplayName: "LockDoor", HasRequest: true},
			{ID: CmdUnlockDoor, Name: "unlock-door", DisplayName: "UnlockDoor", HasRequest: true},
			{ID: CmdUnlockWithTimeout, Name: "unlock-with-timeout", DisplayName: "UnlockWithTimeout", HasRequest: true},
			{ID: CmdSetUser, Name: "set-user", DisplayName: "SetUser", HasRequest: true},
			{ID: CmdGetUser, Name: "get-user", DisplayName: "GetUser", HasRequest: true, HasResponse: true},
			{ID: CmdClearUser, Name: "clear-user", DisplayName: "ClearUser", HasRequest: true},
			{ID: CmdSetCredential, Name: "set-credential", DisplayName: "SetCredential", HasRequest: true, HasResponse: true},
			{ID: CmdGetCredentialStatus, Name: "get-credential-status", DisplayName: "GetCredentialStatus", HasRequest: true, HasResponse: true},
			{ID: CmdClearCredential, Name: "clear-credential", DisplayName: "ClearCredential", HasRequest: true},
		},
	})
}
