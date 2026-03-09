// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package clusters

// ClusterID is a Matter cluster identifier.
type ClusterID = uint32

// AttributeID is a Matter attribute identifier.
type AttributeID = uint32

// CommandID is a Matter command identifier.
type CommandID = uint32

// ClusterInfo describes a Matter cluster for CLI discovery and interaction.
type ClusterInfo struct {
	ID          uint32
	Name        string // PascalCase for CLI usage (e.g. "FanControl")
	DisplayName string // human-friendly name (e.g. "Fan Control")
	Features    []FeatureInfo
	Attributes  []AttributeInfo
	Commands    []CommandInfo
}

// FeatureInfo describes a single feature flag within a cluster's FeatureMap.
type FeatureInfo struct {
	Bit  uint8  // bit position in the FeatureMap bitmap (0-31)
	Code string // short code (e.g. "LT", "PIN")
	Name string // human-friendly name (e.g. "Lighting", "PINCredential")
}

// AttributeInfo describes a single attribute within a cluster.
type AttributeInfo struct {
	ID          uint32
	Name        string // PascalCase (e.g. "FanMode")
	DisplayName string
	Type        string
	Readable    bool
	Writable    bool
	Optional    bool
	Nullable    bool
}

// EnumValue describes a single named value in an enum type.
type EnumValue struct {
	Value uint16
	Name  string
}

// CommandFieldInfo describes a single field within a command request payload.
type CommandFieldInfo struct {
	ID          uint8       // TLV context tag number
	Name        string      // PascalCase for CLI usage (e.g. "IdentifyTime")
	DisplayName string      // human-friendly name (e.g. "IdentifyTime")
	Type        string      // TLV type: "uint8", "uint16", "uint32", "uint64", "int8", "int16", "int32", "int64", "bool", "string", "octets", "enum8", "enum16", "bitmap8", "bitmap16", "bitmap32", "float32", "float64"
	Optional    bool        // whether the field may be omitted
	Nullable    bool        // whether the field may be null
	EnumValues  []EnumValue // named enum values; nil for non-enum fields
}

// CommandInfo describes a single command within a cluster.
type CommandInfo struct {
	ID            uint32
	Name          string // PascalCase (e.g. "Toggle")
	DisplayName   string
	HasRequest    bool
	HasResponse   bool
	RequestFields []CommandFieldInfo // describes the fields in the command request payload; nil/empty for commands with no request
}

// RequiredFields returns only the non-optional fields from RequestFields.
func (ci *CommandInfo) RequiredFields() []CommandFieldInfo {
	var required []CommandFieldInfo
	for _, f := range ci.RequestFields {
		if !f.Optional {
			required = append(required, f)
		}
	}
	return required
}

// FieldByName looks up a request field by name (case-insensitive).
func (ci *CommandInfo) FieldByName(name string) (*CommandFieldInfo, bool) {
	for i := range ci.RequestFields {
		if equalFold(ci.RequestFields[i].Name, name) {
			return &ci.RequestFields[i], true
		}
	}
	return nil, false
}

// equalFold is a simple case-insensitive string comparison.
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
