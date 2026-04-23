// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

// InvokeRequest is the TLV structure for an Invoke Request message (opcode 0x08).
// It carries one or more command invocations to execute on the peer.
type InvokeRequest struct {
	SuppressResponse bool          `tlv:"0,bool"`
	TimedRequest     bool          `tlv:"1,bool"`
	InvokeRequests   []CommandDataIB `tlv:"2,array"`
}

// CommandDataIB carries a single command invocation or response, pairing
// a command path with optional raw TLV-encoded fields.
type CommandDataIB struct {
	Path   CommandPath `tlv:"0,liststruct"`
	Fields []byte      `tlv:"1,rawstruct"`
}

// InvokeResponse is the TLV structure for an Invoke Response message (opcode 0x09).
// It contains per-command response data and/or status results.
type InvokeResponse struct {
	SuppressResponse bool               `tlv:"0,bool"`
	InvokeResponses  []InvokeResponseIB `tlv:"1,array"`
}

// InvokeResponseIB carries either response data or a status for a single
// invoked command. Exactly one of Command or Status is set.
type InvokeResponseIB struct {
	Command *CommandDataIB   `tlv:"0,struct"`
	Status  *CommandStatusIB `tlv:"1,struct"`
}

// CommandStatusIB pairs a command path with a status, used when a command
// invocation fails or succeeds with no response data.
type CommandStatusIB struct {
	Path   CommandPath `tlv:"0,liststruct"`
	Status StatusIB    `tlv:"1,struct"`
}
