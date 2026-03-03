// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package basicinformation implements the Matter Basic Information cluster (0x0028).
package basicinformation

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Basic Information.
	ID uint32 = 0x0028
	// Name is the CLI-friendly cluster name.
	Name = "BasicInformation"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Basic Information"
)

// Attribute IDs.
const (
	AttrDataModelRevision   uint32 = 0x0000
	AttrVendorName          uint32 = 0x0001
	AttrVendorID            uint32 = 0x0002
	AttrProductName         uint32 = 0x0003
	AttrProductID           uint32 = 0x0004
	AttrNodeLabel           uint32 = 0x0005
	AttrLocation            uint32 = 0x0006
	AttrHardwareVersion     uint32 = 0x0007
	AttrHardwareVersionStr  uint32 = 0x0008
	AttrSoftwareVersion     uint32 = 0x0009
	AttrSoftwareVersionStr  uint32 = 0x000A
	AttrManufacturingDate   uint32 = 0x000B
	AttrPartNumber          uint32 = 0x000C
	AttrProductURL          uint32 = 0x000D
	AttrProductLabel        uint32 = 0x000E
	AttrSerialNumber        uint32 = 0x000F
	AttrLocalConfigDisabled uint32 = 0x0010
	AttrReachable           uint32 = 0x0011
	AttrUniqueID            uint32 = 0x0012
)

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrDataModelRevision, Name: "DataModelRevision", DisplayName: "DataModelRevision", Type: "uint16", Readable: true},
			{ID: AttrVendorName, Name: "VendorName", DisplayName: "VendorName", Type: "string", Readable: true},
			{ID: AttrVendorID, Name: "VendorID", DisplayName: "VendorID", Type: "vendor-id", Readable: true},
			{ID: AttrProductName, Name: "ProductName", DisplayName: "ProductName", Type: "string", Readable: true},
			{ID: AttrProductID, Name: "ProductID", DisplayName: "ProductID", Type: "uint16", Readable: true},
			{ID: AttrNodeLabel, Name: "NodeLabel", DisplayName: "NodeLabel", Type: "string", Readable: true, Writable: true},
			{ID: AttrLocation, Name: "Location", DisplayName: "Location", Type: "string", Readable: true, Writable: true},
			{ID: AttrHardwareVersion, Name: "HardwareVersion", DisplayName: "HardwareVersion", Type: "uint16", Readable: true},
			{ID: AttrHardwareVersionStr, Name: "HardwareVersionString", DisplayName: "HardwareVersionString", Type: "string", Readable: true},
			{ID: AttrSoftwareVersion, Name: "SoftwareVersion", DisplayName: "SoftwareVersion", Type: "uint32", Readable: true},
			{ID: AttrSoftwareVersionStr, Name: "SoftwareVersionString", DisplayName: "SoftwareVersionString", Type: "string", Readable: true},
			{ID: AttrManufacturingDate, Name: "ManufacturingDate", DisplayName: "ManufacturingDate", Type: "string", Readable: true, Optional: true},
			{ID: AttrPartNumber, Name: "PartNumber", DisplayName: "PartNumber", Type: "string", Readable: true, Optional: true},
			{ID: AttrProductURL, Name: "ProductURL", DisplayName: "ProductURL", Type: "string", Readable: true, Optional: true},
			{ID: AttrProductLabel, Name: "ProductLabel", DisplayName: "ProductLabel", Type: "string", Readable: true, Optional: true},
			{ID: AttrSerialNumber, Name: "SerialNumber", DisplayName: "SerialNumber", Type: "string", Readable: true, Optional: true},
			{ID: AttrLocalConfigDisabled, Name: "LocalConfigDisabled", DisplayName: "LocalConfigDisabled", Type: "bool", Readable: true, Writable: true, Optional: true},
			{ID: AttrReachable, Name: "Reachable", DisplayName: "Reachable", Type: "bool", Readable: true, Optional: true},
			{ID: AttrUniqueID, Name: "UniqueID", DisplayName: "UniqueID", Type: "string", Readable: true, Optional: true},
		},
	})
}
