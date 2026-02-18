// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package matter provides a high-level Go API for controlling Matter smart
// home devices. It wraps the internal controller, commissioning, discovery,
// and interaction model packages into a single, easy-to-use Client.
//
// Basic usage:
//
//	client, err := matter.NewClient(matter.DefaultConfig())
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
//	// Commission a new device.
//	err = client.Commission(ctx, matter.CommissionParams{
//	    SetupCode: "MT:Y3.13OTB00KA0648G00",
//	    NodeID:    1,
//	})
//
//	// Connect and toggle a light.
//	session, err := client.ConnectCASE(ctx, "192.168.1.42:5540", 1)
//	_, err = session.Invoke(ctx, 1, 0x0006, 0x02, nil) // on-off toggle
package matter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/commissioning"
	"github.com/p0fi/matter-cli/internal/controller"
	"github.com/p0fi/matter-cli/internal/discovery"
	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/p0fi/matter-cli/internal/tlv"

	// Import cluster packages so they register via init().
	_ "github.com/p0fi/matter-cli/internal/clusters/accesscontrol"
	_ "github.com/p0fi/matter-cli/internal/clusters/activatedcarbonfiltermonitoring"
	_ "github.com/p0fi/matter-cli/internal/clusters/airquality"
	_ "github.com/p0fi/matter-cli/internal/clusters/basicinformation"
	_ "github.com/p0fi/matter-cli/internal/clusters/colorcontrol"
	_ "github.com/p0fi/matter-cli/internal/clusters/descriptor"
	_ "github.com/p0fi/matter-cli/internal/clusters/doorlock"
	_ "github.com/p0fi/matter-cli/internal/clusters/fancontrol"
	_ "github.com/p0fi/matter-cli/internal/clusters/generalcommissioning"
	_ "github.com/p0fi/matter-cli/internal/clusters/hepafiltermonitoring"
	_ "github.com/p0fi/matter-cli/internal/clusters/identify"
	_ "github.com/p0fi/matter-cli/internal/clusters/levelcontrol"
	_ "github.com/p0fi/matter-cli/internal/clusters/networkcommissioning"
	_ "github.com/p0fi/matter-cli/internal/clusters/onoff"
	_ "github.com/p0fi/matter-cli/internal/clusters/operationalcredentials"
	_ "github.com/p0fi/matter-cli/internal/clusters/pm10concentrationmeasurement"
	_ "github.com/p0fi/matter-cli/internal/clusters/pm25concentrationmeasurement"
	_ "github.com/p0fi/matter-cli/internal/clusters/thermostat"
	_ "github.com/p0fi/matter-cli/internal/clusters/windowcovering"
)

// Config holds configuration for creating a Client.
type Config struct {
	// StorePath is the filesystem path for persistent storage.
	// If empty, an in-memory store is used.
	StorePath string
	// FabricID is the fabric identity to use. Defaults to 1.
	FabricID uint64
	// BindAddr is the local UDP address to bind to. Defaults to ":0".
	BindAddr string
}

// DefaultConfig returns a Config with sensible defaults. It uses in-memory
// storage, fabric ID 1, and binds to a random port.
func DefaultConfig() Config {
	return Config{
		FabricID: 1,
		BindAddr: ":0",
	}
}

// Client is the main entry point for interacting with Matter devices.
// It manages the underlying transport, session establishment, and
// interaction model operations.
type Client struct {
	ctrl  *controller.Controller
	store store.Store
}

// NewClient creates a new Matter client with the given configuration.
func NewClient(cfg Config) (*Client, error) {
	var s store.Store
	var err error

	if cfg.StorePath != "" {
		s, err = store.NewBoltStore(cfg.StorePath)
		if err != nil {
			return nil, fmt.Errorf("matter: opening store: %w", err)
		}
	} else {
		s = store.NewMemoryStore()
	}

	ctrl, err := controller.New(controller.Config{
		Store:    s,
		FabricID: cfg.FabricID,
		BindAddr: cfg.BindAddr,
	})
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("matter: creating controller: %w", err)
	}

	return &Client{ctrl: ctrl, store: s}, nil
}

// Close shuts down the client, releasing all resources.
func (c *Client) Close() error {
	ctrlErr := c.ctrl.Close()
	storeErr := c.store.Close()
	if ctrlErr != nil {
		return ctrlErr
	}
	return storeErr
}

// CommissionParams holds parameters for commissioning a device.
type CommissionParams struct {
	// SetupCode is the device setup code (QR code "MT:..." or manual pairing code).
	SetupCode string
	// NodeID is the operational node ID to assign to the device.
	NodeID uint64
	// WiFiSSID and WiFiPassword provide WiFi credentials for network commissioning.
	WiFiSSID     string
	WiFiPassword string
	// ThreadDataset provides Thread network credentials for network commissioning.
	ThreadDataset []byte
}

// Commission commissions a new device using its setup code. The device must be
// in commissioning mode and discoverable via mDNS.
func (c *Client) Commission(ctx context.Context, params CommissionParams) error {
	cparams := commissioning.CommissioningParams{
		SetupCode: params.SetupCode,
		NodeID:    params.NodeID,
	}
	if params.WiFiSSID != "" {
		nc := commissioning.NewWiFiCredentials(params.WiFiSSID, params.WiFiPassword)
		cparams.Network = &nc
	} else if len(params.ThreadDataset) > 0 {
		nc := commissioning.NewThreadCredentials(params.ThreadDataset)
		cparams.Network = &nc
	}

	comm := c.ctrl.NewCommissioner()
	_, err := comm.Commission(ctx, cparams)
	return err
}

// CommissionByIP commissions a device at a known IP address, bypassing mDNS
// discovery. This is useful for devices on different subnets or when mDNS is
// unreliable.
func (c *Client) CommissionByIP(ctx context.Context, addr string, passcode uint32, nodeID uint64) error {
	cparams := commissioning.CommissioningParams{
		Passcode: passcode,
		NodeID:   nodeID,
	}

	comm := c.ctrl.NewCommissioner()
	comm.Discoverer = &controller.StaticDiscoverer{Addr: addr}
	_, err := comm.Commission(ctx, cparams)
	return err
}

// ConnectPASE establishes an unauthenticated PASE session with a device.
// This is typically only needed for low-level operations; Commission handles
// PASE automatically.
func (c *Client) ConnectPASE(ctx context.Context, addr string, passcode uint32) (*Session, error) {
	session, err := c.ctrl.ConnectPASE(ctx, addr, passcode)
	if err != nil {
		return nil, err
	}
	return &Session{session: session, client: c}, nil
}

// ConnectCASE establishes an authenticated CASE session with a commissioned
// device identified by its node ID.
func (c *Client) ConnectCASE(ctx context.Context, addr string, nodeID uint64) (*Session, error) {
	session, err := c.ctrl.ConnectCASE(ctx, addr, nodeID)
	if err != nil {
		return nil, err
	}
	return &Session{session: session, client: c}, nil
}

// Session represents an active session with a Matter device.
type Session struct {
	session *protocol.Session
	client  *Client
}

// ReadAttribute reads a single attribute from a device over an established session.
// It returns the raw TLV-encoded attribute value.
func (s *Session) ReadAttribute(ctx context.Context, endpoint uint16, clusterID, attributeID uint32) ([]byte, error) {
	client := interaction.NewClient(s.client.ctrl.Exchanges())
	path := interaction.NewAttributePath(endpoint, clusterID, attributeID)

	reports, err := client.Read(ctx, s.session, path)
	if err != nil {
		return nil, err
	}

	for _, r := range reports {
		if r.Data != nil {
			return r.Data.Data, nil
		}
	}
	return nil, nil
}

// WriteAttribute writes a TLV-encoded value to a single attribute.
func (s *Session) WriteAttribute(ctx context.Context, endpoint uint16, clusterID, attributeID uint32, value []byte) error {
	client := interaction.NewClient(s.client.ctrl.Exchanges())
	path := interaction.NewAttributePath(endpoint, clusterID, attributeID)

	write := interaction.AttributeWrite{
		Path: path,
		Data: value,
	}

	statuses, err := client.Write(ctx, s.session, write)
	if err != nil {
		return err
	}

	for _, st := range statuses {
		if st.Status.Status != 0 {
			return fmt.Errorf("matter: write failed with status %d", st.Status.Status)
		}
	}
	return nil
}

// Invoke sends a command to a device and returns the response fields (if any).
// The request parameter should be a TLV-marshalable struct, or nil for
// commands that take no arguments.
func (s *Session) Invoke(ctx context.Context, endpoint uint16, clusterID, commandID uint32, request any) ([]byte, error) {
	client := interaction.NewClient(s.client.ctrl.Exchanges())

	var fields []byte
	if request != nil {
		var err error
		fields, err = tlv.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("matter: marshaling command request: %w", err)
		}
	}

	path := interaction.CommandPath{
		EndpointID: endpoint,
		ClusterID:  clusterID,
		CommandID:  commandID,
	}

	resp, err := client.Invoke(ctx, s.session, path, fields)
	if err != nil {
		return nil, err
	}

	if resp.Command != nil {
		return resp.Command.Fields, nil
	}
	return nil, nil
}

// Device represents a discovered Matter device on the network.
type Device = discovery.Device

// FindCommissionable scans the local network for commissionable Matter devices.
// The timeout controls how long the mDNS browse runs.
func FindCommissionable(ctx context.Context, timeout time.Duration) ([]*Device, error) {
	browser := discovery.NewMDNSBrowser()
	return browser.DiscoverCommissionable(ctx, timeout)
}

// FindOperational scans the local network for operational Matter devices.
func FindOperational(ctx context.Context, timeout time.Duration) ([]*Device, error) {
	browser := discovery.NewMDNSBrowser()
	return browser.DiscoverOperational(ctx, timeout)
}

// Cluster Registry access for library consumers.

// ClusterInfo describes a registered Matter cluster.
type ClusterInfo = clusters.ClusterInfo

// AttributeInfo describes a single attribute within a cluster.
type AttributeInfo = clusters.AttributeInfo

// CommandInfo describes a single command within a cluster.
type CommandInfo = clusters.CommandInfo

// LookupCluster returns the cluster definition by name (case-insensitive).
func LookupCluster(name string) (*ClusterInfo, bool) {
	return clusters.Global.ClusterByName(name)
}

// LookupClusterByID returns the cluster definition by numeric ID.
func LookupClusterByID(id uint32) (*ClusterInfo, bool) {
	return clusters.Global.ClusterByID(id)
}

// AllClusters returns all registered cluster definitions.
func AllClusters() []ClusterInfo {
	return clusters.Global.AllClusters()
}

// SetupPayload contains the information parsed from a Matter setup code.
type SetupPayload = commissioning.SetupPayload

// ParseSetupCode parses a Matter QR code (starting with "MT:") or a manual
// pairing code (numeric string) and returns the decoded SetupPayload.
func ParseSetupCode(code string) (*SetupPayload, error) {
	code = strings.TrimSpace(code)
	if strings.HasPrefix(code, "MT:") {
		return commissioning.ParseQRCode(code)
	}
	return commissioning.ParseManualPairingCode(code)
}
