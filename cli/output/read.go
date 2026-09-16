// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import "time"

// ReadRecord is one attribute report produced by `cluster read`. Its field
// names deliberately match SubscribeRecord so a consumer that handles one
// attribute report handles both: Value carries the natively typed decoded
// value for JSON/YAML, and Display carries a pre-formatted, possibly
// truncated string used only by the table renderer.
//
// Error is populated when the device answered the path with a status instead
// of data (a privileged attribute, say). Such a record carries no Value —
// "not permitted" is distinguishable from "not present", which shows up as
// the attribute being absent from the result entirely.
type ReadRecord struct {
	Timestamp   time.Time `json:"timestamp" yaml:"timestamp"`
	NodeID      uint64    `json:"node_id" yaml:"node_id"`
	Endpoint    uint16    `json:"endpoint" yaml:"endpoint"`
	ClusterID   uint32    `json:"cluster_id" yaml:"cluster_id"`
	Cluster     string    `json:"cluster" yaml:"cluster"`
	AttributeID uint32    `json:"attribute_id" yaml:"attribute_id"`
	Attribute   string    `json:"attribute" yaml:"attribute"`
	Value       any       `json:"value" yaml:"value"`
	Error       string    `json:"error,omitempty" yaml:"error,omitempty"`
	Raw         string    `json:"raw,omitempty" yaml:"raw,omitempty"`
	DecodeError string    `json:"decode_error,omitempty" yaml:"decode_error,omitempty"`

	// Display is a pre-formatted, human-readable rendering of Value (or of
	// Error), used only by the table renderer. JSON/YAML consumers get Value
	// directly so they never have to parse a display string back into a
	// native type, and never see one truncated for column width.
	Display string `json:"-" yaml:"-"`
}
