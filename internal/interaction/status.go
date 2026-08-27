// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package interaction implements the Matter Interaction Model (IM), providing
// Read, Write, Invoke, and Subscribe operations over Matter exchanges.
// It encodes and decodes IM messages using TLV and communicates through the
// protocol.Exchange abstraction.
package interaction

import (
	"errors"
	"fmt"
)

// ProtocolID is the Matter protocol identifier for the Interaction Model.
const ProtocolID uint16 = 0x0001

// IM opcodes as defined in the Matter specification (Chapter 8).
const (
	OpcodeStatusResponse    byte = 0x01
	OpcodeReadRequest       byte = 0x02
	OpcodeSubscribeRequest  byte = 0x03
	OpcodeSubscribeResponse byte = 0x04
	OpcodeReportData        byte = 0x05
	OpcodeWriteRequest      byte = 0x06
	OpcodeWriteResponse     byte = 0x07
	OpcodeInvokeRequest     byte = 0x08
	OpcodeInvokeResponse    byte = 0x09
	OpcodeTimedRequest      byte = 0x0A
)

// StatusCode represents an Interaction Model status code from the Matter specification.
type StatusCode uint8

const (
	// StatusSuccess indicates the operation completed successfully.
	StatusSuccess StatusCode = 0x00
	// StatusFailure indicates a generic failure.
	StatusFailure StatusCode = 0x01
	// StatusInvalidSubscription indicates an invalid or unknown subscription ID.
	StatusInvalidSubscription StatusCode = 0x7D
	// StatusUnsupportedAccess indicates the access level is insufficient.
	StatusUnsupportedAccess StatusCode = 0x7E
	// StatusUnsupportedEndpoint indicates the endpoint is not supported.
	StatusUnsupportedEndpoint StatusCode = 0x7F
	// StatusInvalidAction indicates the action is not valid in the current state.
	StatusInvalidAction StatusCode = 0x80
	// StatusUnsupportedCommand indicates the command is not supported.
	StatusUnsupportedCommand StatusCode = 0x81
	// StatusInvalidCommand indicates the command is malformed or has invalid fields.
	StatusInvalidCommand StatusCode = 0x85
	// StatusUnsupportedAttribute indicates the attribute is not supported.
	StatusUnsupportedAttribute StatusCode = 0x86
	// StatusConstraintError indicates a value constraint violation.
	StatusConstraintError StatusCode = 0x87
	// StatusUnsupportedWrite indicates the attribute does not support writing.
	StatusUnsupportedWrite StatusCode = 0x88
	// StatusResourceExhausted indicates resources are exhausted.
	StatusResourceExhausted StatusCode = 0x89
	// StatusNotFound indicates the requested resource was not found.
	StatusNotFound StatusCode = 0x8B
	// StatusUnreportableAttribute indicates the attribute cannot be reported.
	StatusUnreportableAttribute StatusCode = 0x8C
	// StatusInvalidDataType indicates a data type mismatch.
	StatusInvalidDataType StatusCode = 0x8D
	// StatusUnsupportedRead indicates the attribute does not support reading.
	StatusUnsupportedRead StatusCode = 0x8F
	// StatusDataVersionMismatch indicates a data version filter mismatch.
	StatusDataVersionMismatch StatusCode = 0x92
	// StatusTimeout indicates the operation timed out.
	StatusTimeout StatusCode = 0x94
	// StatusBusy indicates the node is busy and cannot process the request.
	StatusBusy StatusCode = 0x9C
	// StatusAccessRestricted indicates access to the resource is restricted
	// by an ARL (Access Restriction List) entry.
	StatusAccessRestricted StatusCode = 0x9D
	// StatusUnsupportedCluster indicates the cluster is not supported on the endpoint.
	StatusUnsupportedCluster StatusCode = 0xC3
	// StatusNoUpstreamSubscription indicates there is no upstream subscription
	// to forward the request against.
	StatusNoUpstreamSubscription StatusCode = 0xC5
	// StatusNeedsTimedInteraction indicates the action requires a prior timed
	// invoke or timed write.
	StatusNeedsTimedInteraction StatusCode = 0xC6
	// StatusUnsupportedEvent indicates the event is not supported.
	StatusUnsupportedEvent StatusCode = 0xC7
	// StatusPathsExhausted indicates too many paths in the request.
	StatusPathsExhausted StatusCode = 0xC8
	// StatusTimedRequestMismatch indicates a timed request mismatch.
	StatusTimedRequestMismatch StatusCode = 0xC9
	// StatusFailsafeRequired indicates a failsafe must be armed first.
	StatusFailsafeRequired StatusCode = 0xCA
	// StatusInvalidInState indicates the operation is invalid in the current state.
	StatusInvalidInState StatusCode = 0xCB
	// StatusNoCommandResponse indicates no response was provided for a command.
	StatusNoCommandResponse StatusCode = 0xCC
	// StatusDynamicConstraintError indicates a value constraint violation that
	// depends on dynamic, runtime state rather than a fixed schema constraint.
	StatusDynamicConstraintError StatusCode = 0xCF
	// StatusAlreadyExists indicates the entity being created already exists.
	StatusAlreadyExists StatusCode = 0xD0
	// StatusInvalidTransportType indicates the operation is not valid for the
	// transport type over which the interaction arrived.
	StatusInvalidTransportType StatusCode = 0xD1
)

// statusNames maps status codes to human-readable descriptions.
var statusNames = map[StatusCode]string{
	StatusSuccess:                "SUCCESS",
	StatusFailure:                "FAILURE",
	StatusInvalidSubscription:    "INVALID_SUBSCRIPTION",
	StatusUnsupportedAccess:      "UNSUPPORTED_ACCESS",
	StatusUnsupportedEndpoint:    "UNSUPPORTED_ENDPOINT",
	StatusInvalidAction:          "INVALID_ACTION",
	StatusUnsupportedCommand:     "UNSUPPORTED_COMMAND",
	StatusInvalidCommand:         "INVALID_COMMAND",
	StatusUnsupportedAttribute:   "UNSUPPORTED_ATTRIBUTE",
	StatusConstraintError:        "CONSTRAINT_ERROR",
	StatusUnsupportedWrite:       "UNSUPPORTED_WRITE",
	StatusResourceExhausted:      "RESOURCE_EXHAUSTED",
	StatusNotFound:               "NOT_FOUND",
	StatusUnreportableAttribute:  "UNREPORTABLE_ATTRIBUTE",
	StatusInvalidDataType:        "INVALID_DATA_TYPE",
	StatusUnsupportedRead:        "UNSUPPORTED_READ",
	StatusDataVersionMismatch:    "DATA_VERSION_MISMATCH",
	StatusTimeout:                "TIMEOUT",
	StatusBusy:                   "BUSY",
	StatusAccessRestricted:       "ACCESS_RESTRICTED",
	StatusUnsupportedCluster:     "UNSUPPORTED_CLUSTER",
	StatusNoUpstreamSubscription: "NO_UPSTREAM_SUBSCRIPTION",
	StatusNeedsTimedInteraction:  "NEEDS_TIMED_INTERACTION",
	StatusUnsupportedEvent:       "UNSUPPORTED_EVENT",
	StatusPathsExhausted:         "PATHS_EXHAUSTED",
	StatusTimedRequestMismatch:   "TIMED_REQUEST_MISMATCH",
	StatusFailsafeRequired:       "FAILSAFE_REQUIRED",
	StatusInvalidInState:         "INVALID_IN_STATE",
	StatusNoCommandResponse:      "NO_COMMAND_RESPONSE",
	StatusDynamicConstraintError: "DYNAMIC_CONSTRAINT_ERROR",
	StatusAlreadyExists:          "ALREADY_EXISTS",
	StatusInvalidTransportType:   "INVALID_TRANSPORT_TYPE",
}

// String returns the standard Matter specification name for the status
// code, or "UNKNOWN" if the code is reserved, deprecated, or otherwise not
// part of the current standard Interaction Model status catalog.
func (s StatusCode) String() string {
	if name, ok := statusNames[s]; ok {
		return name
	}
	return "UNKNOWN"
}

// FormatStatus renders an Interaction Model status using the canonical,
// name-first "NAME (0xNN)" contract, with an optional cluster-specific
// status appended as ", cluster status 0xNN". It is the single formatter
// shared by every interaction call site so status output is consistent
// across direct CASE and session-daemon transports.
func FormatStatus(code StatusCode, clusterCode *uint8) string {
	s := fmt.Sprintf("%s (0x%02X)", code, uint8(code))
	if clusterCode != nil {
		s += fmt.Sprintf(", cluster status 0x%02X", *clusterCode)
	}
	return s
}

// StatusIB represents the Status Information Block as defined in the Matter spec.
// It contains a general status code and an optional cluster-specific status.
type StatusIB struct {
	Status        uint8  `tlv:"0,uint"`
	ClusterStatus *uint8 `tlv:"1,uint"`
}

// StatusResponseMessage is the TLV payload of a StatusResponse message (opcode 0x01).
// Note: the Status field is a plain uint8 status code, NOT a nested StatusIB struct.
type StatusResponseMessage struct {
	Status uint8 `tlv:"0,uint"`
}

// StatusError wraps an IM status code into a Go error. It is returned when
// the peer responds with a non-success status.
type StatusError struct {
	// GeneralCode is the IM-level status code.
	GeneralCode StatusCode
	// ClusterCode is the optional cluster-specific status code.
	ClusterCode *uint8
}

// NewStatusError builds a *StatusError from a raw general status code and
// optional cluster-specific status. It is the single construction point used
// throughout the codebase so a general status and its cluster status are
// always paired into the same typed representation, regardless of which wire
// shape (session-daemon JSON, direct-CASE StatusIB) they were read from.
func NewStatusError(code uint8, clusterCode *uint8) *StatusError {
	return &StatusError{
		GeneralCode: StatusCode(code),
		ClusterCode: clusterCode,
	}
}

// Error returns the canonical name-first status representation, e.g.
// "CONSTRAINT_ERROR (0x87)" or "FAILURE (0x01), cluster status 0x03".
func (e *StatusError) Error() string {
	return FormatStatus(e.GeneralCode, e.ClusterCode)
}

// IsStatus returns true if err (or any error in its chain) is a StatusError
// with the given code.  It uses errors.As so it works with wrapped errors too.
func IsStatus(err error, code StatusCode) bool {
	var se *StatusError
	if !errors.As(err, &se) {
		return false
	}
	return se.GeneralCode == code
}

// WrapStatus returns an error whose Error() is exactly msg, with se available
// through Unwrap(). It lets command-specific decoders keep concise,
// actionable wording in normal output while remaining discoverable through
// errors.As / IsStatus as the underlying typed status error.
func WrapStatus(msg string, se *StatusError) error {
	return &wrappedStatusError{msg: msg, err: se}
}

// wrappedStatusError pairs a concise, user-facing message with an underlying
// typed status error that remains reachable via Unwrap.
type wrappedStatusError struct {
	msg string
	err error
}

// Error returns the concise, user-facing message, without the underlying
// status error's own text appended.
func (e *wrappedStatusError) Error() string { return e.msg }

// Unwrap exposes the underlying typed status error for errors.As / errors.Is.
func (e *wrappedStatusError) Unwrap() error { return e.err }

// statusFromIB creates a StatusError from a StatusIB, or returns nil if
// the status indicates success.
func statusFromIB(ib StatusIB) error {
	code := StatusCode(ib.Status)
	if code == StatusSuccess {
		return nil
	}
	return &StatusError{
		GeneralCode: code,
		ClusterCode: ib.ClusterStatus,
	}
}

// statusFromCode creates a StatusError from a raw status code, or returns nil
// if the code indicates success.
func statusFromCode(code uint8) error {
	if StatusCode(code) == StatusSuccess {
		return nil
	}
	return &StatusError{GeneralCode: StatusCode(code)}
}
