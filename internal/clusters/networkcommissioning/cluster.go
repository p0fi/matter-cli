// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package networkcommissioning implements the Matter Network Commissioning cluster (0x0031).
package networkcommissioning

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Network Commissioning.
	ID uint32 = 0x0031
	// Name is the CLI-friendly cluster name.
	Name = "NetworkCommissioning"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Network Commissioning"
)

// Attribute IDs.
const (
	AttrMaxNetworks           uint32 = 0x0000
	AttrNetworks              uint32 = 0x0001
	AttrScanMaxTimeSeconds    uint32 = 0x0002
	AttrConnectMaxTimeSeconds uint32 = 0x0003
	AttrInterfaceEnabled      uint32 = 0x0004
	AttrLastNetworkingStatus  uint32 = 0x0005
	AttrLastNetworkID         uint32 = 0x0006
	AttrLastConnectErrorValue uint32 = 0x0007
)

// Command IDs.
const (
	CmdScanNetworks              uint32 = 0x00
	CmdScanNetworksResponse      uint32 = 0x01
	CmdAddOrUpdateWiFiNetwork    uint32 = 0x02
	CmdAddOrUpdateThreadNetwork  uint32 = 0x03
	CmdRemoveNetwork             uint32 = 0x04
	CmdNetworkConfigResponse     uint32 = 0x05
	CmdConnectNetwork            uint32 = 0x06
	CmdConnectNetworkResponse    uint32 = 0x07
	CmdReorderNetwork            uint32 = 0x08
)

// ScanNetworksRequest is the request payload for the ScanNetworks command.
type ScanNetworksRequest struct {
	SSID       []byte `tlv:"0,octets"`
	Breadcrumb uint64 `tlv:"1,uint"`
}

// AddOrUpdateWiFiNetworkRequest is the request payload for AddOrUpdateWiFiNetwork.
type AddOrUpdateWiFiNetworkRequest struct {
	SSID        []byte `tlv:"0,octets"`
	Credentials []byte `tlv:"1,octets"`
	Breadcrumb  uint64 `tlv:"2,uint"`
}

// AddOrUpdateThreadNetworkRequest is the request payload for AddOrUpdateThreadNetwork.
type AddOrUpdateThreadNetworkRequest struct {
	OperationalDataset []byte `tlv:"0,octets"`
	Breadcrumb         uint64 `tlv:"1,uint"`
}

// RemoveNetworkRequest is the request payload for RemoveNetwork.
type RemoveNetworkRequest struct {
	NetworkID  []byte `tlv:"0,octets"`
	Breadcrumb uint64 `tlv:"1,uint"`
}

// ConnectNetworkRequest is the request payload for ConnectNetwork.
type ConnectNetworkRequest struct {
	NetworkID  []byte `tlv:"0,octets"`
	Breadcrumb uint64 `tlv:"1,uint"`
}

// NetworkConfigResponse is the response payload for network config commands.
type NetworkConfigResponse struct {
	NetworkingStatus uint8  `tlv:"0,uint"`
	DebugText        string `tlv:"1,utf8"`
	NetworkIndex     uint8  `tlv:"2,uint"`
}

// ConnectNetworkResponse is the response payload for ConnectNetwork.
type ConnectNetworkResponse struct {
	NetworkingStatus uint8  `tlv:"0,uint"`
	DebugText        string `tlv:"1,utf8"`
	ErrorValue       int32  `tlv:"2,int"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrMaxNetworks, Name: "MaxNetworks", DisplayName: "MaxNetworks", Type: "uint8", Readable: true},
			{ID: AttrNetworks, Name: "Networks", DisplayName: "Networks", Type: "list[struct]", Readable: true},
			{ID: AttrScanMaxTimeSeconds, Name: "ScanMaxTimeSeconds", DisplayName: "ScanMaxTimeSeconds", Type: "uint8", Readable: true},
			{ID: AttrConnectMaxTimeSeconds, Name: "ConnectMaxTimeSeconds", DisplayName: "ConnectMaxTimeSeconds", Type: "uint8", Readable: true},
			{ID: AttrInterfaceEnabled, Name: "InterfaceEnabled", DisplayName: "InterfaceEnabled", Type: "bool", Readable: true, Writable: true},
			{ID: AttrLastNetworkingStatus, Name: "LastNetworkingStatus", DisplayName: "LastNetworkingStatus", Type: "enum8", Readable: true, Nullable: true},
			{ID: AttrLastNetworkID, Name: "LastNetworkID", DisplayName: "LastNetworkID", Type: "octets", Readable: true, Nullable: true},
			{ID: AttrLastConnectErrorValue, Name: "LastConnectErrorValue", DisplayName: "LastConnectErrorValue", Type: "int32", Readable: true, Nullable: true},
		},
		Commands: []clusters.CommandInfo{
			{ID: CmdScanNetworks, Name: "ScanNetworks", DisplayName: "ScanNetworks", HasRequest: true, HasResponse: true},
			{ID: CmdAddOrUpdateWiFiNetwork, Name: "AddOrUpdateWiFiNetwork", DisplayName: "AddOrUpdateWiFiNetwork", HasRequest: true, HasResponse: true},
			{ID: CmdAddOrUpdateThreadNetwork, Name: "AddOrUpdateThreadNetwork", DisplayName: "AddOrUpdateThreadNetwork", HasRequest: true, HasResponse: true},
			{ID: CmdRemoveNetwork, Name: "RemoveNetwork", DisplayName: "RemoveNetwork", HasRequest: true, HasResponse: true},
			{ID: CmdConnectNetwork, Name: "ConnectNetwork", DisplayName: "ConnectNetwork", HasRequest: true, HasResponse: true},
			{ID: CmdReorderNetwork, Name: "ReorderNetwork", DisplayName: "ReorderNetwork", HasRequest: true, HasResponse: true},
		},
	})
}
