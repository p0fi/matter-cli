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
	Name        string // kebab-case for CLI usage
	DisplayName string // human-friendly name
	Attributes  []AttributeInfo
	Commands    []CommandInfo
}

// AttributeInfo describes a single attribute within a cluster.
type AttributeInfo struct {
	ID          uint32
	Name        string // kebab-case
	DisplayName string
	Type        string
	Readable    bool
	Writable    bool
	Optional    bool
	Nullable    bool
}

// CommandFieldInfo describes a single field within a command request payload.
type CommandFieldInfo struct {
	ID          uint8  // TLV context tag number
	Name        string // kebab-case for CLI usage (e.g. "identify-time")
	DisplayName string // human-friendly name (e.g. "IdentifyTime")
	Type        string // TLV type: "uint8", "uint16", "uint32", "uint64", "int8", "int16", "int32", "int64", "bool", "string", "octets", "enum8", "enum16", "bitmap8", "bitmap16", "bitmap32", "float32", "float64"
	Optional    bool   // whether the field may be omitted
	Nullable    bool   // whether the field may be null
}

// CommandInfo describes a single command within a cluster.
type CommandInfo struct {
	ID            uint32
	Name          string // kebab-case
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

// FieldByName looks up a request field by its kebab-case CLI name (case-insensitive).
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
