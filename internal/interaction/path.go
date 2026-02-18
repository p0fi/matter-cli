// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import "fmt"

// AttributePath identifies an attribute on a Matter node. Nil pointer fields
// act as wildcards, matching all values for that path component.
type AttributePath struct {
	EnableTagCompression *bool   `tlv:"0,bool"`
	NodeID               *uint64 `tlv:"1,uint"`
	EndpointID           *uint16 `tlv:"2,uint"`
	ClusterID            *uint32 `tlv:"3,uint"`
	AttributeID          *uint32 `tlv:"4,uint"`
	ListIndex            *uint16 `tlv:"5,uint"`
}

// NewAttributePath creates an AttributePath targeting a specific endpoint,
// cluster, and attribute.
func NewAttributePath(endpoint uint16, cluster uint32, attribute uint32) AttributePath {
	return AttributePath{
		EndpointID:  &endpoint,
		ClusterID:   &cluster,
		AttributeID: &attribute,
	}
}

// String returns a human-readable representation of the attribute path.
func (p AttributePath) String() string {
	ep := "*"
	if p.EndpointID != nil {
		ep = fmt.Sprintf("%d", *p.EndpointID)
	}
	cl := "*"
	if p.ClusterID != nil {
		cl = fmt.Sprintf("0x%04X", *p.ClusterID)
	}
	at := "*"
	if p.AttributeID != nil {
		at = fmt.Sprintf("0x%04X", *p.AttributeID)
	}
	return fmt.Sprintf("EP:%s/CL:%s/AT:%s", ep, cl, at)
}

// EventPath identifies an event on a Matter node. Nil pointer fields
// act as wildcards.
type EventPath struct {
	NodeID     *uint64 `tlv:"1,uint"`
	EndpointID *uint16 `tlv:"2,uint"`
	ClusterID  *uint32 `tlv:"3,uint"`
	EventID    *uint32 `tlv:"4,uint"`
	IsUrgent   *bool   `tlv:"5,bool"`
}

// String returns a human-readable representation of the event path.
func (p EventPath) String() string {
	ep := "*"
	if p.EndpointID != nil {
		ep = fmt.Sprintf("%d", *p.EndpointID)
	}
	cl := "*"
	if p.ClusterID != nil {
		cl = fmt.Sprintf("0x%04X", *p.ClusterID)
	}
	ev := "*"
	if p.EventID != nil {
		ev = fmt.Sprintf("0x%04X", *p.EventID)
	}
	return fmt.Sprintf("EP:%s/CL:%s/EV:%s", ep, cl, ev)
}

// CommandPath identifies a command on a specific endpoint and cluster.
// All fields are required (no wildcards).
type CommandPath struct {
	EndpointID uint16 `tlv:"0,uint"`
	ClusterID  uint32 `tlv:"1,uint"`
	CommandID  uint32 `tlv:"2,uint"`
}

// NewCommandPath creates a CommandPath with the given endpoint, cluster, and command IDs.
func NewCommandPath(endpoint uint16, cluster uint32, command uint32) CommandPath {
	return CommandPath{
		EndpointID: endpoint,
		ClusterID:  cluster,
		CommandID:  command,
	}
}

// String returns a human-readable representation of the command path.
func (p CommandPath) String() string {
	return fmt.Sprintf("EP:%d/CL:0x%04X/CMD:0x%04X", p.EndpointID, p.ClusterID, p.CommandID)
}
