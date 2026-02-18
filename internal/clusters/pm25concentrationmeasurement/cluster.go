// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package pm25concentrationmeasurement implements the Matter PM2.5 Concentration Measurement cluster (0x042A).
package pm25concentrationmeasurement

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for PM2.5 Concentration Measurement.
	ID uint32 = 0x042A
	// Name is the CLI-friendly cluster name.
	Name = "pm25-concentration-measurement"
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
			{ID: AttrMeasuredValue, Name: "measured-value", DisplayName: "MeasuredValue", Type: "float32", Readable: true, Nullable: true, Optional: true},
			{ID: AttrMinMeasuredValue, Name: "min-measured-value", DisplayName: "MinMeasuredValue", Type: "float32", Readable: true, Nullable: true, Optional: true},
			{ID: AttrMaxMeasuredValue, Name: "max-measured-value", DisplayName: "MaxMeasuredValue", Type: "float32", Readable: true, Nullable: true, Optional: true},
			{ID: AttrPeakMeasuredValue, Name: "peak-measured-value", DisplayName: "PeakMeasuredValue", Type: "float32", Readable: true, Nullable: true, Optional: true},
			{ID: AttrPeakMeasuredValueWindow, Name: "peak-measured-value-window", DisplayName: "PeakMeasuredValueWindow", Type: "uint32", Readable: true, Optional: true},
			{ID: AttrAverageMeasuredValue, Name: "average-measured-value", DisplayName: "AverageMeasuredValue", Type: "float32", Readable: true, Nullable: true, Optional: true},
			{ID: AttrAverageMeasuredValueWindow, Name: "average-measured-value-window", DisplayName: "AverageMeasuredValueWindow", Type: "uint32", Readable: true, Optional: true},
			{ID: AttrUncertainty, Name: "uncertainty", DisplayName: "Uncertainty", Type: "float32", Readable: true, Optional: true},
			{ID: AttrMeasurementUnit, Name: "measurement-unit", DisplayName: "MeasurementUnit", Type: "enum8", Readable: true, Optional: true},
			{ID: AttrMeasurementMedium, Name: "measurement-medium", DisplayName: "MeasurementMedium", Type: "enum8", Readable: true},
			{ID: AttrLevelValue, Name: "level-value", DisplayName: "LevelValue", Type: "enum8", Readable: true, Optional: true},
		},
	})
}
