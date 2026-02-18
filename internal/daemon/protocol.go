// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package daemon implements a background session daemon that keeps Matter CASE
// sessions alive across CLI invocations. The daemon listens on a Unix domain
// socket and accepts JSON-encoded requests from CLI processes, executing them
// against cached sessions and returning results without the overhead of
// re-establishing CASE for every command.
package daemon

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Request is the top-level JSON envelope sent from the CLI to the daemon.
type Request struct {
	// Type identifies the kind of request. One of: "ping", "invoke", "read",
	// "write", "subscribe", "shutdown", "status".
	Type string `json:"type"`

	// NodeID is the target node for device operations.
	NodeID uint64 `json:"node_id,omitempty"`

	// FabricID is the fabric to operate under. Defaults to 1 if zero.
	FabricID uint64 `json:"fabric_id,omitempty"`

	// KeepAlive is the duration the daemon should stay alive (reset on each
	// request). Only meaningful when establishing a new session; ignored if the
	// daemon is already running with its own timeout.
	KeepAlive Duration `json:"keep_alive,omitempty"`

	// Invoke fields.
	Invoke *InvokeReq `json:"invoke,omitempty"`

	// Read fields.
	Read *ReadReq `json:"read,omitempty"`

	// Write fields.
	Write *WriteReq `json:"write,omitempty"`

	// Subscribe fields.
	Subscribe *SubscribeReq `json:"subscribe,omitempty"`
}

// InvokeReq carries the parameters for an invoke request.
type InvokeReq struct {
	Endpoint  uint16 `json:"endpoint"`
	ClusterID uint32 `json:"cluster_id"`
	CommandID uint32 `json:"command_id"`
	// Fields is the raw TLV-encoded command fields, base64-encoded for JSON
	// transport. May be empty for commands with no request payload.
	Fields string `json:"fields,omitempty"`
	// TimedMs is the timed interaction timeout in milliseconds. Zero means
	// no timed interaction.
	TimedMs uint16 `json:"timed_ms,omitempty"`
}

// ReadReq carries the parameters for a read request.
type ReadReq struct {
	Paths []AttrPathReq `json:"paths"`
}

// AttrPathReq identifies an attribute to read or subscribe to.
type AttrPathReq struct {
	Endpoint    uint16 `json:"endpoint"`
	ClusterID   uint32 `json:"cluster_id"`
	AttributeID uint32 `json:"attribute_id"`
}

// WriteReq carries the parameters for a write request.
type WriteReq struct {
	Writes []AttrWriteReq `json:"writes"`
}

// AttrWriteReq identifies an attribute to write along with the TLV-encoded value.
type AttrWriteReq struct {
	Endpoint    uint16 `json:"endpoint"`
	ClusterID   uint32 `json:"cluster_id"`
	AttributeID uint32 `json:"attribute_id"`
	// Data is the raw TLV-encoded attribute value, base64-encoded.
	Data string `json:"data"`
}

// SubscribeReq carries the parameters for a subscribe request.
type SubscribeReq struct {
	Paths       []AttrPathReq `json:"paths"`
	MinInterval uint16        `json:"min_interval"`
	MaxInterval uint16        `json:"max_interval"`
}

// Response is the top-level JSON envelope sent from the daemon back to the CLI.
type Response struct {
	// OK is true when the request was processed successfully.
	OK bool `json:"ok"`

	// Error contains a human-readable error message when OK is false.
	Error string `json:"error,omitempty"`

	// Invoke result.
	Invoke *InvokeResp `json:"invoke,omitempty"`

	// Read result.
	Read *ReadResp `json:"read,omitempty"`

	// Write result.
	Write *WriteResp `json:"write,omitempty"`

	// Status result (for "status" and "ping" requests).
	Status *StatusResp `json:"status,omitempty"`
}

// InvokeResp carries the result of an invoke request.
type InvokeResp struct {
	// StatusCode is the IM status code. 0 means success.
	StatusCode uint8 `json:"status_code"`
	// Data is the base64-encoded raw TLV response fields, if any.
	Data string `json:"data,omitempty"`
	// HasData is true when the invoke returned response data (as opposed to
	// just a status).
	HasData bool `json:"has_data,omitempty"`
}

// ReadResp carries the result of a read request.
type ReadResp struct {
	Reports []AttrReportResp `json:"reports"`
}

// AttrReportResp is a single attribute report in a read response.
type AttrReportResp struct {
	Endpoint    uint16 `json:"endpoint"`
	ClusterID   uint32 `json:"cluster_id"`
	AttributeID uint32 `json:"attribute_id"`
	// Data is the base64-encoded raw TLV attribute value.
	Data string `json:"data"`
	// StatusCode is non-zero if this particular attribute returned an error.
	StatusCode uint8 `json:"status_code,omitempty"`
}

// WriteResp carries the result of a write request.
type WriteResp struct {
	Statuses []AttrStatusResp `json:"statuses"`
}

// AttrStatusResp is the status for a single attribute write.
type AttrStatusResp struct {
	Endpoint    uint16 `json:"endpoint"`
	ClusterID   uint32 `json:"cluster_id"`
	AttributeID uint32 `json:"attribute_id"`
	StatusCode  uint8  `json:"status_code"`
}

// StatusResp carries daemon status information.
type StatusResp struct {
	// Running is true when the daemon is alive.
	Running bool `json:"running"`
	// Uptime is how long the daemon has been running.
	Uptime Duration `json:"uptime"`
	// IdleTimeout is the remaining time before the daemon auto-exits.
	IdleTimeout Duration `json:"idle_timeout"`
	// Sessions lists the currently cached sessions.
	Sessions []SessionInfo `json:"sessions,omitempty"`
}

// SessionInfo describes a single cached CASE session.
type SessionInfo struct {
	NodeID      uint64 `json:"node_id"`
	SessionID   uint16 `json:"session_id"`
	PeerAddress string `json:"peer_address"`
	Established Duration `json:"established"`
}

// Duration is a time.Duration that marshals to/from JSON as a human-readable
// string (e.g. "5m0s") instead of a raw nanosecond integer.
type Duration time.Duration

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", time.Duration(d).String())), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(b []byte) error {
	// Strip surrounding quotes.
	s := string(b)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("daemon: parsing duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// D returns the underlying time.Duration.
func (d Duration) D() time.Duration {
	return time.Duration(d)
}

// EncodeFields base64-encodes raw TLV bytes for JSON transport.
func EncodeFields(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeFields decodes base64-encoded TLV bytes from JSON transport.
func DecodeFields(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

// SocketPath returns the path to the daemon's Unix domain socket.
// It respects XDG_CONFIG_HOME and falls back to ~/.config/matter-cli/.
func SocketPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".", ".matter-cli", "daemon.sock")
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "matter-cli", "daemon.sock")
}

// PidPath returns the path to the daemon's PID file.
func PidPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".", ".matter-cli", "daemon.pid")
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "matter-cli", "daemon.pid")
}
