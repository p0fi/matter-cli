// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package pm25concentrationmeasurement implements the Matter PM2.5 Concentration Measurement cluster (0x042A).
package pm25concentrationmeasurement

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for PM2.5 Concentration Measurement.
	ID uint32 = 0x042A
	// Name is the CLI-friendly cluster name.
	Name = "PM25ConcentrationMeasurement"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "PM2.5 Concentration Measurement"
)

// Attribute IDs.
const (
	AttrMeasuredValue              uint32 = 0x0000
	AttrMinMeasuredValue           uint32 = 0x0001
	AttrMaxMeasuredValue           uint32 = 0x0002
	AttrPeakMeasuredValue          uint32 = 0x0003
	AttrPeakMeasuredValueWindow    uint32 = 0x0004
	AttrAverageMeasuredValue       uint32 = 0x0005
	AttrAverageMeasuredValueWindow uint32 = 0x0006
	AttrUncertainty                uint32 = 0x0007
	AttrMeasurementUnit            uint32 = 0x0008
	AttrMeasurementMedium          uint32 = 0x0009
	AttrLevelValue                 uint32 = 0x000A
)

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrMeasuredValue, Name: "MeasuredValue", DisplayName: "MeasuredValue", Type: "float32", Readable: true, Nullable: true, Optional: true},
			{ID: AttrMinMeasuredValue, Name: "MinMeasuredValue", DisplayName: "MinMeasuredValue", Type: "float32", Readable: true, Nullable: true, Optional: true},
			{ID: AttrMaxMeasuredValue, Name: "MaxMeasuredValue", DisplayName: "MaxMeasuredValue", Type: "float32", Readable: true, Nullable: true, Optional: true},
			{ID: AttrPeakMeasuredValue, Name: "PeakMeasuredValue", DisplayName: "PeakMeasuredValue", Type: "float32", Readable: true, Nullable: true, Optional: true},
			{ID: AttrPeakMeasuredValueWindow, Name: "PeakMeasuredValueWindow", DisplayName: "PeakMeasuredValueWindow", Type: "uint32", Readable: true, Optional: true},
			{ID: AttrAverageMeasuredValue, Name: "AverageMeasuredValue", DisplayName: "AverageMeasuredValue", Type: "float32", Readable: true, Nullable: true, Optional: true},
			{ID: AttrAverageMeasuredValueWindow, Name: "AverageMeasuredValueWindow", DisplayName: "AverageMeasuredValueWindow", Type: "uint32", Readable: true, Optional: true},
			{ID: AttrUncertainty, Name: "Uncertainty", DisplayName: "Uncertainty", Type: "float32", Readable: true, Optional: true},
			{ID: AttrMeasurementUnit, Name: "MeasurementUnit", DisplayName: "MeasurementUnit", Type: "enum8", Readable: true, Optional: true},
			{ID: AttrMeasurementMedium, Name: "MeasurementMedium", DisplayName: "MeasurementMedium", Type: "enum8", Readable: true},
			{ID: AttrLevelValue, Name: "LevelValue", DisplayName: "LevelValue", Type: "enum8", Readable: true, Optional: true},
		},
	})
}
