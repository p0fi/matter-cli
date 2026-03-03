// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package operationalcredentials implements the Matter Operational Credentials cluster (0x003E).
package operationalcredentials

import "github.com/p0fi/matter-cli/internal/clusters"

const (
	// ID is the Matter cluster ID for Operational Credentials.
	ID uint32 = 0x003E
	// Name is the CLI-friendly cluster name.
	Name = "OperationalCredentials"
	// DisplayName is the human-friendly cluster name.
	DisplayName = "Operational Credentials"
)

// Attribute IDs.
const (
	AttrNOCs                 uint32 = 0x0000
	AttrFabrics              uint32 = 0x0001
	AttrSupportedFabrics     uint32 = 0x0002
	AttrCommissionedFabrics  uint32 = 0x0003
	AttrTrustedRootCerts     uint32 = 0x0004
	AttrCurrentFabricIndex   uint32 = 0x0005
)

// Command IDs.
const (
	CmdAttestationRequest     uint32 = 0x00
	CmdAttestationResponse    uint32 = 0x01
	CmdCertificateChainReq    uint32 = 0x02
	CmdCertificateChainResp   uint32 = 0x03
	CmdCSRRequest             uint32 = 0x04
	CmdCSRResponse            uint32 = 0x05
	CmdAddNOC                 uint32 = 0x06
	CmdUpdateNOC              uint32 = 0x07
	CmdNOCResponse            uint32 = 0x08
	CmdUpdateFabricLabel      uint32 = 0x09
	CmdRemoveFabric           uint32 = 0x0A
	CmdAddTrustedRootCert     uint32 = 0x0B
)

// AttestationRequest is the request payload for the AttestationRequest command.
type AttestationRequest struct {
	AttestationNonce []byte `tlv:"0,octets"`
}

// AttestationResponse is the response payload for the AttestationResponse command.
type AttestationResponse struct {
	AttestationElements []byte `tlv:"0,octets"`
	AttestationSignature []byte `tlv:"1,octets"`
}

// CSRRequest is the request payload for the CSRRequest command.
type CSRRequest struct {
	CSRNonce    []byte `tlv:"0,octets"`
	IsForUpdate bool   `tlv:"1,bool"`
}

// CSRResponse is the response payload for the CSRResponse command.
type CSRResponse struct {
	NOCSRElements []byte `tlv:"0,octets"`
	AttestationSignature []byte `tlv:"1,octets"`
}

// AddNOCRequest is the request payload for the AddNOC command.
type AddNOCRequest struct {
	NOCValue      []byte `tlv:"0,octets"`
	ICACValue     []byte `tlv:"1,octets"`
	IPKValue      []byte `tlv:"2,octets"`
	CaseAdminSubject uint64 `tlv:"3,uint"`
	AdminVendorId    uint16 `tlv:"4,uint"`
}

// NOCResponse is the response payload for NOC-related commands.
type NOCResponse struct {
	StatusCode  uint8  `tlv:"0,uint"`
	FabricIndex uint8  `tlv:"1,uint"`
	DebugText   string `tlv:"2,utf8"`
}

// AddTrustedRootCertRequest is the request payload for AddTrustedRootCertificate.
type AddTrustedRootCertRequest struct {
	RootCACertificate []byte `tlv:"0,octets"`
}

func init() {
	clusters.Global.Register(clusters.ClusterInfo{
		ID:          ID,
		Name:        Name,
		DisplayName: DisplayName,
		Attributes: []clusters.AttributeInfo{
			{ID: AttrNOCs, Name: "NOCs", DisplayName: "NOCs", Type: "list[struct]", Readable: true},
			{ID: AttrFabrics, Name: "Fabrics", DisplayName: "Fabrics", Type: "list[struct]", Readable: true},
			{ID: AttrSupportedFabrics, Name: "SupportedFabrics", DisplayName: "SupportedFabrics", Type: "uint8", Readable: true},
			{ID: AttrCommissionedFabrics, Name: "CommissionedFabrics", DisplayName: "CommissionedFabrics", Type: "uint8", Readable: true},
			{ID: AttrTrustedRootCerts, Name: "TrustedRootCertificates", DisplayName: "TrustedRootCertificates", Type: "list[octets]", Readable: true},
			{ID: AttrCurrentFabricIndex, Name: "CurrentFabricIndex", DisplayName: "CurrentFabricIndex", Type: "uint8", Readable: true},
		},
		Commands: []clusters.CommandInfo{
			{ID: CmdAttestationRequest, Name: "AttestationRequest", DisplayName: "AttestationRequest", HasRequest: true, HasResponse: true},
			{ID: CmdCertificateChainReq, Name: "CertificateChainRequest", DisplayName: "CertificateChainRequest", HasRequest: true, HasResponse: true},
			{ID: CmdCSRRequest, Name: "CSRRequest", DisplayName: "CSRRequest", HasRequest: true, HasResponse: true},
			{ID: CmdAddNOC, Name: "AddNOC", DisplayName: "AddNOC", HasRequest: true, HasResponse: true},
			{ID: CmdUpdateNOC, Name: "UpdateNOC", DisplayName: "UpdateNOC", HasRequest: true, HasResponse: true},
			{ID: CmdUpdateFabricLabel, Name: "UpdateFabricLabel", DisplayName: "UpdateFabricLabel", HasRequest: true, HasResponse: true},
			{ID: CmdRemoveFabric, Name: "RemoveFabric", DisplayName: "RemoveFabric", HasRequest: true, HasResponse: true},
			{ID: CmdAddTrustedRootCert, Name: "AddTrustedRootCertificate", DisplayName: "AddTrustedRootCertificate", HasRequest: true},
		},
	})
}
