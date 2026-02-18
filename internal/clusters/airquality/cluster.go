// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package airquality implements the Matter Air Quality cluster (0x005B).
package airquality

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Air Quality.
	ID uint32 = 0x005B
	// Name is the CLI-friendly cluster name.
	Name = "air-quality"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Air Quality"
)

// Attribute IDs.
const (
	AttrAirQuality uint32 = 0x0000
)

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrAirQuality, Name: "air-quality", DisplayName: "AirQuality", Type: "enum8", Readable: true},
		},
	})
}
