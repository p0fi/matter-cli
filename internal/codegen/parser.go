// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package codegen parses Alchemy-generated Matter cluster XML definitions
// and generates Go source files for the internal/clusters registry.
package codegen

import (
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ---------- XML schema types ----------

// XMLCluster is the root <cluster> element.
type XMLCluster struct {
	XMLName    xml.Name       `xml:"cluster"`
	ID         string         `xml:"id,attr"`
	Name       string         `xml:"name,attr"`
	ClusterIDs XMLClusterIDs  `xml:"clusterIds"`
	Features   XMLFeatures    `xml:"features"`
	DataTypes  XMLDataTypes   `xml:"dataTypes"`
	Attributes []XMLAttribute `xml:"attributes>attribute"`
	Commands   []XMLCommand   `xml:"commands>command"`
}

// XMLFeatures contains the <features> element.
type XMLFeatures struct {
	Features []XMLFeature `xml:"feature"`
}

// XMLFeature is a single <feature> element.
type XMLFeature struct {
	Bit     string `xml:"bit,attr"`
	Code    string `xml:"code,attr"`
	Name    string `xml:"name,attr"`
	Summary string `xml:"summary,attr"`
}

// XMLClusterIDs contains one or more <clusterId> entries.
type XMLClusterIDs struct {
	IDs []XMLClusterID `xml:"clusterId"`
}

// XMLClusterID is a single <clusterId> element.
type XMLClusterID struct {
	ID   string `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

// XMLDataTypes contains enums, bitmaps, and structs.
type XMLDataTypes struct {
	Enums   []XMLEnum   `xml:"enum"`
	Bitmaps []XMLBitmap `xml:"bitmap"`
	Structs []XMLStruct `xml:"struct"`
}

// XMLEnum is an <enum> data type.
type XMLEnum struct {
	Name  string        `xml:"name,attr"`
	Items []XMLEnumItem `xml:"item"`
}

// XMLEnumItem is a single <item> within an enum.
type XMLEnumItem struct {
	Value string `xml:"value,attr"`
	Name  string `xml:"name,attr"`
}

// XMLBitmap is a <bitmap> data type.
type XMLBitmap struct {
	Name      string        `xml:"name,attr"`
	Bitfields []XMLBitfield `xml:"bitfield"`
}

// XMLBitfield is a single <bitfield> within a bitmap.
type XMLBitfield struct {
	Name string `xml:"name,attr"`
	Bit  string `xml:"bit,attr"`
}

// XMLStruct is a <struct> data type.
type XMLStruct struct {
	Name   string     `xml:"name,attr"`
	Fields []XMLField `xml:"field"`
}

// XMLAttribute is an <attribute> element.
type XMLAttribute struct {
	ID      string     `xml:"id,attr"`
	Name    string     `xml:"name,attr"`
	Type    string     `xml:"type,attr"`
	Access  XMLAccess  `xml:"access"`
	Quality XMLQuality `xml:"quality"`
}

// XMLCommand is a <command> element.
type XMLCommand struct {
	ID        string     `xml:"id,attr"`
	Name      string     `xml:"name,attr"`
	Direction string     `xml:"direction,attr"`
	Response  string     `xml:"response,attr"`
	Fields    []XMLField `xml:"field"`
}

// XMLField is a <field> element inside a command or struct.
type XMLField struct {
	ID              string         `xml:"id,attr"`
	Name            string         `xml:"name,attr"`
	Type            string         `xml:"type,attr"`
	Quality         XMLQuality     `xml:"quality"`
	OptionalConform *XMLOptConform `xml:"optionalConform"`
}

// XMLOptConform exists when the field has <optionalConform/>.
type XMLOptConform struct{}

// XMLAccess describes read/write access on an attribute.
type XMLAccess struct {
	Read  string `xml:"read,attr"`
	Write string `xml:"write,attr"`
}

// XMLQuality describes quality flags on an attribute or field.
type XMLQuality struct {
	Nullable string `xml:"nullable,attr"`
}

// ---------- Parsed (canonical) model ----------

// Cluster is the fully parsed, canonical representation of a single Matter cluster.
type Cluster struct {
	ID          uint32
	Name        string // PascalCase, no spaces (e.g. "OnOff")
	DisplayName string // human-friendly (e.g. "On/Off")
	PackageName string // lowercase Go package name (e.g. "onoff")
	Features    []Feature
	Attributes  []Attribute
	Commands    []Command
	// DataType lookup maps (name → kind) for resolving attribute/field types.
	EnumNames   map[string]bool
	BitmapNames map[string]bool
}

// Feature is a parsed feature flag from the cluster's FeatureMap.
type Feature struct {
	Bit  uint8  // bit position (0-31)
	Code string // short code (e.g. "LT")
	Name string // human-friendly name (e.g. "Lighting")
}

// Attribute is a parsed cluster attribute.
type Attribute struct {
	ID       uint32
	Name     string // PascalCase
	Type     string // matter type string for registry (e.g. "uint8", "enum8", "bitmap16")
	Readable bool
	Writable bool
	Nullable bool
	Optional bool
}

// Command is a parsed cluster command (client→server only).
type Command struct {
	ID          uint32
	Name        string // PascalCase
	HasRequest  bool
	HasResponse bool
	Fields      []CommandField
}

// EnumVal is a named value within an enum data type.
type EnumVal struct {
	Value uint16
	Name  string
}

// CommandField is a parsed field inside a command request.
type CommandField struct {
	ID         uint8
	Name       string // PascalCase
	Type       string // matter type for registry
	GoType     string // Go type for struct field
	TLVKind    string // TLV tag kind: "uint", "int", "bool", "utf8", "bytes"
	Optional   bool
	Nullable   bool
	EnumValues []EnumVal // populated when the field references a named enum type
}

// ParseFile parses a single Alchemy cluster XML file and returns one or more Clusters
// (some XMLs like ConcentrationMeasurement define multiple cluster IDs).
func ParseFile(path string) ([]Cluster, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(data)
}

// Parse parses XML bytes into one or more Clusters.
func Parse(data []byte) ([]Cluster, error) {
	var xc XMLCluster
	if err := xml.Unmarshal(data, &xc); err != nil {
		return nil, fmt.Errorf("unmarshal XML: %w", err)
	}

	// Build data type lookup maps.
	enumNames := make(map[string]bool, len(xc.DataTypes.Enums))
	enumItems := make(map[string][]EnumVal, len(xc.DataTypes.Enums))
	for _, e := range xc.DataTypes.Enums {
		enumNames[e.Name] = true
		for _, item := range e.Items {
			v, _ := parseHexOrDec(item.Value)
			enumItems[e.Name] = append(enumItems[e.Name], EnumVal{Value: uint16(v), Name: item.Name})
		}
	}
	bitmapNames := make(map[string]bool, len(xc.DataTypes.Bitmaps))
	for _, b := range xc.DataTypes.Bitmaps {
		bitmapNames[b.Name] = true
	}

	// Parse features.
	features := make([]Feature, 0, len(xc.Features.Features))
	for _, xf := range xc.Features.Features {
		bit, err := strconv.ParseUint(xf.Bit, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("feature %s bit: %w", xf.Name, err)
		}
		features = append(features, Feature{
			Bit:  uint8(bit),
			Code: xf.Code,
			Name: xf.Name,
		})
	}

	// Parse attributes (shared across all cluster IDs in this XML).
	attrs := make([]Attribute, 0, len(xc.Attributes))
	for _, xa := range xc.Attributes {
		id, err := parseHexOrDec(xa.ID)
		if err != nil {
			return nil, fmt.Errorf("attribute %s ID: %w", xa.Name, err)
		}
		attrs = append(attrs, Attribute{
			ID:       id,
			Name:     xa.Name,
			Type:     resolveType(xa.Type, enumNames, bitmapNames),
			Readable: xa.Access.Read == "true",
			Writable: xa.Access.Write == "true",
			Nullable: xa.Quality.Nullable == "true",
		})
	}

	// Parse commands (only commandToServer).
	cmds := make([]Command, 0, len(xc.Commands))
	for _, xcmd := range xc.Commands {
		if xcmd.Direction != "commandToServer" {
			continue
		}
		id, err := parseHexOrDec(xcmd.ID)
		if err != nil {
			return nil, fmt.Errorf("command %s ID: %w", xcmd.Name, err)
		}
		cmd := Command{
			ID:          id,
			Name:        xcmd.Name,
			HasRequest:  len(xcmd.Fields) > 0,
			HasResponse: xcmd.Response != "" && xcmd.Response != "Y",
		}
		for _, xf := range xcmd.Fields {
			fid, err := parseHexOrDec(xf.ID)
			if err != nil {
				return nil, fmt.Errorf("command %s field %s ID: %w", xcmd.Name, xf.Name, err)
			}
			matterType := resolveType(xf.Type, enumNames, bitmapNames)
			cf := CommandField{
				ID:       uint8(fid),
				Name:     xf.Name,
				Type:     matterType,
				GoType:   matterTypeToGo(matterType),
				TLVKind:  matterTypeToTLVKind(matterType),
				Optional: xf.OptionalConform != nil,
				Nullable: xf.Quality.Nullable == "true",
			}
			if vals, ok := enumItems[xf.Type]; ok {
				cf.EnumValues = vals
			}
			cmd.Fields = append(cmd.Fields, cf)
		}
		cmds = append(cmds, cmd)
	}

	// Build one Cluster per clusterId entry.
	ids := xc.ClusterIDs.IDs
	if len(ids) == 0 {
		// Fallback: use the root cluster's id+name attributes.
		if xc.ID == "" {
			return nil, fmt.Errorf("no cluster IDs found in XML")
		}
		ids = []XMLClusterID{{ID: xc.ID, Name: cleanClusterName(xc.Name)}}
	}

	clusters := make([]Cluster, 0, len(ids))
	for _, cid := range ids {
		if cid.ID == "" {
			// Base/abstract cluster with no ID (e.g. AlarmBase, ModeBase) — skip.
			continue
		}
		id, err := parseHexOrDec(cid.ID)
		if err != nil {
			return nil, fmt.Errorf("cluster %s ID: %w", cid.Name, err)
		}
		displayName := cid.Name
		pascalName := toPascalCase(displayName)
		pkgName := sanitizePackageName(strings.ToLower(pascalName))
		clusters = append(clusters, Cluster{
			ID:          id,
			Name:        pascalName,
			DisplayName: displayName,
			PackageName: pkgName,
			Features:    features,
			Attributes:  attrs,
			Commands:    cmds,
			EnumNames:   enumNames,
			BitmapNames: bitmapNames,
		})
	}

	return clusters, nil
}

// ---------- helpers ----------

// parseHexOrDec parses "0x0006" or "6" into uint32.
func parseHexOrDec(s string) (uint32, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		v, err := strconv.ParseUint(s[2:], 16, 32)
		return uint32(v), err
	}
	v, err := strconv.ParseUint(s, 10, 32)
	return uint32(v), err
}

// cleanClusterName strips a trailing " Cluster" suffix.
func cleanClusterName(name string) string {
	name = strings.TrimSuffix(name, " Cluster")
	return name
}

// toPascalCase converts "On/Off" → "OnOff", "Fan Control" → "FanControl",
// "PM2.5 Concentration Measurement" → "PM25ConcentrationMeasurement", etc.
func toPascalCase(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			if upper {
				b.WriteRune(r - ('a' - 'A'))
				upper = false
			} else {
				b.WriteRune(r)
			}
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
			upper = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			upper = false
		default:
			// spaces, slashes, dots, hyphens → next letter uppercase
			upper = true
		}
	}
	return b.String()
}

// goKeywords is the set of Go reserved keywords that cannot be used as package names.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// sanitizePackageName ensures the package name is a valid Go identifier.
// Reserved keywords get a "cluster" suffix (e.g. "switch" → "switchcluster").
func sanitizePackageName(name string) string {
	if goKeywords[name] {
		return name + "cluster"
	}
	return name
}

// resolveType maps an XML type string to a canonical matter type string.
func resolveType(xmlType string, enums, bitmaps map[string]bool) string {
	// Check named types first.
	if enums[xmlType] {
		return "enum8"
	}
	if bitmaps[xmlType] {
		return "bitmap8" // default; overridden below for wider bitmaps
	}

	switch xmlType {
	case "bool":
		return "bool"
	case "uint8":
		return "uint8"
	case "uint16":
		return "uint16"
	case "uint24":
		return "uint32"
	case "uint32":
		return "uint32"
	case "uint48":
		return "uint64"
	case "uint56":
		return "uint64"
	case "uint64":
		return "uint64"
	case "int8":
		return "int8"
	case "int16":
		return "int16"
	case "int24":
		return "int32"
	case "int32":
		return "int32"
	case "int64":
		return "int64"
	case "single":
		return "float32"
	case "double":
		return "float64"
	case "octstr", "octets", "ipv4adr", "ipv6adr", "hwadr":
		return "octets"
	case "string", "char_string", "long_char_string":
		return "string"
	case "enum8":
		return "enum8"
	case "enum16":
		return "enum16"
	case "bitmap8":
		return "bitmap8"
	case "bitmap16":
		return "bitmap16"
	case "bitmap32":
		return "bitmap32"
	case "bitmap64":
		return "bitmap64"
	case "percent":
		return "uint8"
	case "percent100ths":
		return "uint16"
	case "elapsed-s":
		return "uint32"
	case "epoch-us":
		return "uint64"
	case "epoch-s":
		return "uint32"
	case "posix-ms":
		return "uint64"
	case "systime-us":
		return "uint64"
	case "systime-ms":
		return "uint64"
	case "temperature":
		return "int16"
	case "power-mW":
		return "int64"
	case "amperage-mA":
		return "int64"
	case "voltage-mV":
		return "int64"
	case "energy-mWh":
		return "int64"
	case "fabric-idx":
		return "uint8"
	case "action-id":
		return "uint8"
	case "cluster-id":
		return "uint32"
	case "attrib-id":
		return "uint32"
	case "command-id":
		return "uint32"
	case "event-id":
		return "uint32"
	case "field-id":
		return "uint32"
	case "endpoint-no":
		return "uint16"
	case "group-id":
		return "uint16"
	case "vendor-id":
		return "uint16"
	case "devtype-id":
		return "uint32"
	case "fabric-id":
		return "uint64"
	case "node-id":
		return "uint64"
	case "subject-id", "SubjectID":
		return "uint64"
	case "entry-idx":
		return "uint16"
	case "data-ver":
		return "uint32"
	case "tod":
		return "uint32"
	case "date":
		return "uint32"
	case "status":
		return "uint8"
	case "priority":
		return "uint8"
	case "list":
		return "list"
	case "map16":
		return "bitmap16"
	case "int16s":
		return "int16"
	case "attribute-id":
		return "uint32"
	case "ipv6pre":
		return "octets"
	case "energy-mVARh", "energy-mVAh":
		return "int64"
	case "power-mVA", "power-mVAR":
		return "int64"
	case "systemtime-us":
		return "uint64"
	case "tag":
		return "uint32"
	case "message-id":
		return "octets"
	case "money":
		return "int64"
	default:
		// Unknown named types from dataTypes — treat as enum8 if it ends with Enum,
		// bitmap8 if ends with Bitmap, otherwise uint8 as safe fallback.
		if strings.HasSuffix(xmlType, "Enum") {
			return "enum8"
		}
		if strings.HasSuffix(xmlType, "Bitmap") {
			return "bitmap8"
		}
		if strings.HasSuffix(xmlType, "Struct") {
			return "struct"
		}
		// For list-of or ref types, return as-is. Generator will handle.
		return "uint8"
	}
}

// matterTypeToGo converts a matter type to a Go type for struct fields.
func matterTypeToGo(matterType string) string {
	switch matterType {
	case "bool":
		return "bool"
	case "uint8", "enum8", "bitmap8":
		return "uint8"
	case "uint16", "enum16", "bitmap16":
		return "uint16"
	case "uint32", "bitmap32":
		return "uint32"
	case "uint64", "bitmap64":
		return "uint64"
	case "int8":
		return "int8"
	case "int16":
		return "int16"
	case "int32":
		return "int32"
	case "int64":
		return "int64"
	case "float32":
		return "float32"
	case "float64":
		return "float64"
	case "string":
		return "string"
	case "octets":
		return "[]byte"
	default:
		return "uint8"
	}
}

// matterTypeToTLVKind returns the TLV tag kind suffix for struct tags.
func matterTypeToTLVKind(matterType string) string {
	switch matterType {
	case "bool":
		return "bool"
	case "int8", "int16", "int32", "int64":
		return "int"
	case "string":
		return "utf8"
	case "octets":
		return "bytes"
	default:
		return "uint"
	}
}
