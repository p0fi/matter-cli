// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"time"

	"github.com/p0fi/matter-cli/internal/tlv"
)

// CommissioningParams holds parameters for commissioning a device.
type CommissioningParams struct {
	// SetupCode is the device setup code (QR code "MT:..." or manual pairing code).
	// If Passcode is set, SetupCode is not used.
	SetupCode string
	// Passcode is the device setup passcode (27-bit). If set, SetupCode parsing
	// is skipped and this passcode is used directly.
	Passcode uint32
	// Discriminator is the 12-bit device discriminator for mDNS discovery.
	// Only needed if Passcode is set and mDNS discovery is used.
	Discriminator uint16
	// NodeID is the operational node ID to assign to the device.
	NodeID uint64
	// Network holds optional network credentials for WiFi or Thread provisioning.
	// If nil, the device is assumed to be on an Ethernet network.
	Network *NetworkCredentials
	// FailsafeSeconds is the failsafe timer duration. Defaults to 60 if zero.
	FailsafeSeconds uint16
}

// CommissioningStep identifies a step in the commissioning flow for progress reporting.
type CommissioningStep int

const (
	StepParseSetupCode CommissioningStep = iota
	StepDiscover
	StepEstablishPASE
	StepArmFailsafe
	StepReadBasicInfo
	StepAttestationRequest
	StepValidateAttestation
	StepCSRRequest
	StepGenerateNOC
	StepAddTrustedRoot
	StepAddNOC
	StepNetworkSetup
	StepNetworkConnect
	StepCommissioningComplete
	StepEstablishCASE
)

// String returns a human-readable name for the commissioning step.
func (s CommissioningStep) String() string {
	names := [...]string{
		"ParseSetupCode",
		"Discover",
		"EstablishPASE",
		"ArmFailsafe",
		"ReadBasicInfo",
		"AttestationRequest",
		"ValidateAttestation",
		"CSRRequest",
		"GenerateNOC",
		"AddTrustedRoot",
		"AddNOC",
		"NetworkSetup",
		"NetworkConnect",
		"CommissioningComplete",
		"EstablishCASE",
	}
	if int(s) < len(names) {
		return names[s]
	}
	return fmt.Sprintf("Step(%d)", int(s))
}

// DeviceDiscoverer abstracts mDNS/DNS-SD device discovery.
type DeviceDiscoverer interface {
	// DiscoverCommissionable finds a commissionable device matching the given
	// discriminator. Returns the device address (host:port).
	DiscoverCommissionable(ctx context.Context, discriminator uint16) (addr string, err error)
}

// SessionEstablisher abstracts PASE and CASE session establishment.
type SessionEstablisher interface {
	// EstablishPASE establishes a PASE session with the device at the given
	// address using the provided passcode. Returns a session handle.
	EstablishPASE(ctx context.Context, addr string, passcode uint32) (Session, error)

	// EstablishCASE establishes a CASE session with the device at the given
	// address using the provided node credentials. Returns a session handle.
	EstablishCASE(ctx context.Context, addr string, nodeID uint64) (Session, error)
}

// Session represents an established session with a device.
type Session interface {
	// Close terminates the session.
	Close() error
}

// InteractionClient abstracts IM Read/Write/Invoke operations.
type InteractionClient interface {
	// Invoke sends a command and returns the response fields.
	Invoke(ctx context.Context, session Session, endpoint uint16, cluster, command uint32, request []byte) ([]byte, error)

	// InvokeTimed sends a Timed Invoke (required by certain commands like
	// AddTrustedRootCertificate and AddNOC). timeoutMs is the timed request
	// timeout in milliseconds.
	InvokeTimed(ctx context.Context, session Session, endpoint uint16, cluster, command uint32, request []byte, timeoutMs uint16) ([]byte, error)

	// ReadAttribute reads an attribute and returns the value as raw TLV.
	ReadAttribute(ctx context.Context, session Session, endpoint uint16, cluster, attribute uint32) ([]byte, error)
}

// NOCIssuer abstracts the generation of Node Operational Certificates.
type NOCIssuer interface {
	// IssueNOC generates a NOC for the given CSR and node ID.
	// Returns the NOC (DER), ICAC (DER, may be nil), IPK, root cert (DER), and admin subject.
	IssueNOC(csrElements []byte, nodeID uint64) (noc, icac, ipk, rootCert []byte, adminSubject uint64, err error)
}

// ProgressFunc is called during commissioning to report progress.
type ProgressFunc func(step CommissioningStep)

// Commissioner orchestrates the full Matter commissioning flow.
type Commissioner struct {
	Discoverer  DeviceDiscoverer
	Sessions    SessionEstablisher
	Client      InteractionClient
	NOCIssuer   NOCIssuer
	Attestation AttestationValidator
	OnProgress  ProgressFunc
}

// CommissioningResult contains device information gathered during commissioning.
type CommissioningResult struct {
	VendorName  string
	VendorID    uint16
	ProductName string
	ProductID   uint16
	Address     string         // host:port used to reach the device
	Endpoints   []EndpointInfo // discovered endpoints and their clusters
}

// EndpointInfo describes a single endpoint discovered via the Descriptor cluster.
type EndpointInfo struct {
	ID          uint16
	DeviceTypes []DeviceTypeInfo
	ServerClusters []uint32
}

// DeviceTypeInfo describes a device type entry from the Descriptor cluster.
type DeviceTypeInfo struct {
	ID       uint32
	Revision uint16
}

// Commission performs the full commissioning flow for a device.
func (c *Commissioner) Commission(ctx context.Context, params CommissioningParams) (*CommissioningResult, error) {
	if params.FailsafeSeconds == 0 {
		params.FailsafeSeconds = 60
	}

	// Step 1: Determine passcode and discriminator.
	var passcode uint32
	var discriminator uint16
	if params.Passcode != 0 {
		// Direct passcode provided — skip setup code parsing.
		passcode = params.Passcode
		discriminator = params.Discriminator
	} else {
		c.reportProgress(StepParseSetupCode)
		payload, err := parseSetupCode(params.SetupCode)
		if err != nil {
			return nil, fmt.Errorf("commissioning: parsing setup code: %w", err)
		}
		passcode = payload.Passcode
		discriminator = payload.Discriminator
		shortDisc := discriminator&0xFF == 0
		slog.Debug("commissioning: parsed setup code",
			"passcode", passcode,
			"discriminator", fmt.Sprintf("0x%03X (%d)", discriminator, discriminator),
			"shortDiscriminator", shortDisc,
			"vendorID", fmt.Sprintf("0x%04X", payload.VendorID),
			"productID", fmt.Sprintf("0x%04X", payload.ProductID),
			"flow", payload.CommissioningFlow,
			"discoveryCapabilities", fmt.Sprintf("0x%02X", payload.DiscoveryCapabilities),
		)
	}

	// Step 2: Discover device.
	c.reportProgress(StepDiscover)
	addr, err := c.Discoverer.DiscoverCommissionable(ctx, discriminator)
	if err != nil {
		return nil, fmt.Errorf("commissioning: discovering device: %w", err)
	}

	// Step 3: Establish PASE session.
	c.reportProgress(StepEstablishPASE)
	paseSession, err := c.Sessions.EstablishPASE(ctx, addr, passcode)
	if err != nil {
		return nil, fmt.Errorf("commissioning: establishing PASE session: %w", err)
	}
	defer paseSession.Close()

	// Step 4: Arm failsafe.
	c.reportProgress(StepArmFailsafe)
	if err := c.armFailsafe(ctx, paseSession, params.FailsafeSeconds); err != nil {
		return nil, fmt.Errorf("commissioning: arming failsafe: %w", err)
	}

	// Step 5: Read BasicInformation (sanity check over PASE).
	c.reportProgress(StepReadBasicInfo)
	if _, err := c.Client.ReadAttribute(ctx, paseSession, 0, 0x0028, 0x0001); err != nil {
		return nil, fmt.Errorf("commissioning: reading basic information: %w", err)
	}

	// Step 6: Attestation request.
	c.reportProgress(StepAttestationRequest)
	attestation, dacChain, err := c.requestAttestation(ctx, paseSession)
	if err != nil {
		return nil, fmt.Errorf("commissioning: requesting attestation: %w", err)
	}

	// Step 7: Validate attestation.
	c.reportProgress(StepValidateAttestation)
	if c.Attestation != nil {
		if _, err := c.Attestation.ValidateDAC(dacChain); err != nil {
			return nil, fmt.Errorf("commissioning: validating DAC chain: %w", err)
		}
		if err := c.Attestation.ValidateAttestation(attestation, dacChain.DAC, attestation.Nonce); err != nil {
			return nil, fmt.Errorf("commissioning: validating attestation: %w", err)
		}
	}

	// Step 8: CSR request.
	c.reportProgress(StepCSRRequest)
	csrResp, err := c.requestCSR(ctx, paseSession)
	if err != nil {
		return nil, fmt.Errorf("commissioning: requesting CSR: %w", err)
	}

	// Step 9: Generate NOC.
	c.reportProgress(StepGenerateNOC)
	noc, icac, ipk, rootCert, adminSubject, err := c.NOCIssuer.IssueNOC(csrResp, params.NodeID)
	if err != nil {
		return nil, fmt.Errorf("commissioning: issuing NOC: %w", err)
	}

	// Step 10: Add trusted root certificate.
	c.reportProgress(StepAddTrustedRoot)
	if err := c.addTrustedRoot(ctx, paseSession, rootCert); err != nil {
		return nil, fmt.Errorf("commissioning: adding trusted root: %w", err)
	}

	// Step 11: Add NOC.
	c.reportProgress(StepAddNOC)
	if err := c.addNOC(ctx, paseSession, noc, icac, ipk, adminSubject); err != nil {
		return nil, fmt.Errorf("commissioning: adding NOC: %w", err)
	}

	// Step 12-13: Network setup (if needed).
	if params.Network != nil && params.Network.Type != NetworkEthernet {
		c.reportProgress(StepNetworkSetup)
		if err := c.setupNetwork(ctx, paseSession, params.Network); err != nil {
			return nil, fmt.Errorf("commissioning: setting up network: %w", err)
		}

		c.reportProgress(StepNetworkConnect)
		if err := c.connectNetwork(ctx, paseSession, params.Network); err != nil {
			return nil, fmt.Errorf("commissioning: connecting network: %w", err)
		}
	}

	// Step 14: Establish CASE session.
	// CommissioningComplete must be sent over a CASE session (not PASE).
	// After AddNOC the device is ready to accept CASE connections.
	c.reportProgress(StepEstablishCASE)

	var caseSession Session
	var caseErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			slog.Debug("commissioning: retrying CASE", "attempt", attempt+1)
		}
		// Wait before each attempt to give the device time to transition.
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		caseSession, caseErr = c.Sessions.EstablishCASE(ctx, addr, params.NodeID)
		if caseErr == nil {
			break
		}
		slog.Debug("commissioning: CASE attempt failed", "attempt", attempt+1, "err", caseErr)
	}
	if caseErr != nil {
		return nil, fmt.Errorf("commissioning: establishing CASE session: %w", caseErr)
	}
	defer caseSession.Close()

	// Step 15: CommissioningComplete over the CASE session.
	c.reportProgress(StepCommissioningComplete)
	if err := c.commissioningComplete(ctx, caseSession); err != nil {
		return nil, fmt.Errorf("commissioning: completing commissioning: %w", err)
	}

	// Read device info over the CASE session now that commissioning is complete.
	result := &CommissioningResult{Address: addr}
	c.readDeviceInfo(ctx, caseSession, result)

	return result, nil
}

// readDeviceInfo reads BasicInformation and Descriptor cluster attributes to
// populate the result. Errors are logged but not fatal — the device is already
// commissioned.
func (c *Commissioner) readDeviceInfo(ctx context.Context, session Session, result *CommissioningResult) {
	const basicInfo = 0x0028  // BasicInformation cluster
	const descriptor = 0x001D // Descriptor cluster

	if data, err := c.Client.ReadAttribute(ctx, session, 0, basicInfo, 0x0001); err == nil {
		result.VendorName = decodeTLVString(data)
	}
	if data, err := c.Client.ReadAttribute(ctx, session, 0, basicInfo, 0x0002); err == nil {
		result.VendorID = decodeTLVUint16(data)
	}
	if data, err := c.Client.ReadAttribute(ctx, session, 0, basicInfo, 0x0003); err == nil {
		result.ProductName = decodeTLVString(data)
	}
	if data, err := c.Client.ReadAttribute(ctx, session, 0, basicInfo, 0x0004); err == nil {
		result.ProductID = decodeTLVUint16(data)
	}

	// Read endpoint structure from Descriptor cluster.
	// PartsList (attr 0x0003) on endpoint 0 lists all non-root endpoints.
	endpoints := []uint16{0}
	if data, err := c.Client.ReadAttribute(ctx, session, 0, descriptor, 0x0003); err == nil {
		endpoints = append(endpoints, decodeTLVUint16Array(data)...)
	} else {
		slog.Debug("commissioning: failed to read PartsList", "err", err)
	}

	for _, ep := range endpoints {
		info := EndpointInfo{ID: ep}

		// DeviceTypeList (attr 0x0000): array of structs {tag0: deviceType, tag1: revision}.
		if data, err := c.Client.ReadAttribute(ctx, session, ep, descriptor, 0x0000); err == nil {
			info.DeviceTypes = decodeDeviceTypeList(data)
		}

		// ServerList (attr 0x0001): array of cluster IDs.
		if data, err := c.Client.ReadAttribute(ctx, session, ep, descriptor, 0x0001); err == nil {
			info.ServerClusters = decodeTLVUint32Array(data)
		}

		result.Endpoints = append(result.Endpoints, info)
	}
}

// decodeTLVString extracts a UTF-8 string from a raw TLV element.
func decodeTLVString(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	r := tlv.NewReader(bytes.NewReader(data))
	if err := r.Next(); err != nil {
		return ""
	}
	if s, ok := r.Value().(string); ok {
		return s
	}
	return ""
}

// decodeTLVUint16 extracts a uint16 from a raw TLV element.
func decodeTLVUint16(data []byte) uint16 {
	if len(data) == 0 {
		return 0
	}
	r := tlv.NewReader(bytes.NewReader(data))
	if err := r.Next(); err != nil {
		return 0
	}
	if v, ok := r.Value().(uint64); ok {
		return uint16(v)
	}
	return 0
}

// decodeTLVUint16Array decodes a TLV array of unsigned integers into a []uint16.
func decodeTLVUint16Array(data []byte) []uint16 {
	if len(data) == 0 {
		return nil
	}
	r := tlv.NewReader(bytes.NewReader(data))
	// Enter the array container.
	if err := r.Next(); err != nil {
		return nil
	}
	if r.Type() != tlv.TypeArray {
		return nil
	}

	var result []uint16
	for {
		if err := r.Next(); err != nil {
			break
		}
		if r.Type() == tlv.TypeEndOfContainer {
			break
		}
		if v, ok := r.Value().(uint64); ok {
			result = append(result, uint16(v))
		}
	}
	return result
}

// decodeTLVUint32Array decodes a TLV array of unsigned integers into a []uint32.
func decodeTLVUint32Array(data []byte) []uint32 {
	if len(data) == 0 {
		return nil
	}
	r := tlv.NewReader(bytes.NewReader(data))
	if err := r.Next(); err != nil {
		return nil
	}
	if r.Type() != tlv.TypeArray {
		return nil
	}

	var result []uint32
	for {
		if err := r.Next(); err != nil {
			break
		}
		if r.Type() == tlv.TypeEndOfContainer {
			break
		}
		if v, ok := r.Value().(uint64); ok {
			result = append(result, uint32(v))
		}
	}
	return result
}

// decodeDeviceTypeList decodes a TLV array of DeviceType structs.
// Each element is a struct with tag 0 = device type ID, tag 1 = revision.
func decodeDeviceTypeList(data []byte) []DeviceTypeInfo {
	if len(data) == 0 {
		return nil
	}
	r := tlv.NewReader(bytes.NewReader(data))
	if err := r.Next(); err != nil {
		return nil
	}
	if r.Type() != tlv.TypeArray {
		return nil
	}

	var result []DeviceTypeInfo
	for {
		if err := r.Next(); err != nil {
			break
		}
		if r.Type() == tlv.TypeEndOfContainer {
			break
		}
		if r.Type() != tlv.TypeStructure {
			continue
		}

		var dt DeviceTypeInfo
		for {
			if err := r.Next(); err != nil {
				break
			}
			if r.Type() == tlv.TypeEndOfContainer {
				break
			}
			tag := r.TagValue()
			if v, ok := r.Value().(uint64); ok {
				switch tag {
				case tlv.ContextTag(0):
					dt.ID = uint32(v)
				case tlv.ContextTag(1):
					dt.Revision = uint16(v)
				}
			}
		}
		result = append(result, dt)
	}
	return result
}

func (c *Commissioner) reportProgress(step CommissioningStep) {
	if c.OnProgress != nil {
		c.OnProgress(step)
	}
}

// parseSetupCode parses either a QR code or manual pairing code.
func parseSetupCode(code string) (*SetupPayload, error) {
	if len(code) > 3 && code[:3] == "MT:" {
		return ParseQRCode(code)
	}
	return ParseManualPairingCode(code)
}

func (c *Commissioner) armFailsafe(ctx context.Context, session Session, seconds uint16) error {
	// ArmFailSafe command: cluster 0x0030, command 0x00
	req, encErr := encodeArmFailsafe(seconds)
	if encErr != nil {
		return fmt.Errorf("encoding ArmFailSafe: %w", encErr)
	}
	resp, err := c.Client.Invoke(ctx, session, 0, 0x0030, 0x00, req)
	if err != nil {
		return fmt.Errorf("invoking ArmFailSafe: %w", err)
	}
	return checkCommandErrorCode(resp, "ArmFailSafe")
}

func (c *Commissioner) requestAttestation(ctx context.Context, session Session) (AttestationInfo, DACChain, error) {
	// Generate a random nonce (32 bytes).
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return AttestationInfo{}, DACChain{}, fmt.Errorf("generating attestation nonce: %w", err)
	}

	// AttestationRequest: cluster 0x003E, command 0x00
	attReq, err := encodeOctetStringField(0, nonce)
	if err != nil {
		return AttestationInfo{}, DACChain{}, fmt.Errorf("encoding AttestationRequest: %w", err)
	}
	resp, err := c.Client.Invoke(ctx, session, 0, 0x003E, 0x00, attReq)
	if err != nil {
		return AttestationInfo{}, DACChain{}, fmt.Errorf("invoking AttestationRequest: %w", err)
	}

	// Request DAC: CertificateChainRequest with type=1 (DAC).
	dacReq, err := encodeUintField(0, 1)
	if err != nil {
		return AttestationInfo{}, DACChain{}, fmt.Errorf("encoding DAC request: %w", err)
	}
	dacResp, err := c.Client.Invoke(ctx, session, 0, 0x003E, 0x02, dacReq)
	if err != nil {
		return AttestationInfo{}, DACChain{}, fmt.Errorf("requesting DAC: %w", err)
	}

	// Request PAI: CertificateChainRequest with type=2 (PAI).
	paiReq, err := encodeUintField(0, 2)
	if err != nil {
		return AttestationInfo{}, DACChain{}, fmt.Errorf("encoding PAI request: %w", err)
	}
	paiResp, err := c.Client.Invoke(ctx, session, 0, 0x003E, 0x02, paiReq)
	if err != nil {
		return AttestationInfo{}, DACChain{}, fmt.Errorf("requesting PAI: %w", err)
	}

	info := AttestationInfo{
		Elements:  resp,
		Signature: resp, // TODO: parse AttestationResponse TLV properly
		Nonce:     nonce,
	}

	chain := DACChain{
		DAC: dacResp,
		PAI: paiResp,
	}

	return info, chain, nil
}

func (c *Commissioner) requestCSR(ctx context.Context, session Session) ([]byte, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generating CSR nonce: %w", err)
	}

	// CSRRequest: cluster 0x003E, command 0x04.
	csrReq, err := encodeOctetStringField(0, nonce)
	if err != nil {
		return nil, fmt.Errorf("encoding CSRRequest: %w", err)
	}
	resp, err := c.Client.Invoke(ctx, session, 0, 0x003E, 0x04, csrReq)
	if err != nil {
		return nil, fmt.Errorf("invoking CSRRequest: %w", err)
	}
	return resp, nil
}

func (c *Commissioner) addTrustedRoot(ctx context.Context, session Session, rootCert []byte) error {
	// AddTrustedRootCertificate: cluster 0x003E, command 0x0B.
	// This command requires a Timed Invoke per the Matter spec.
	slog.Debug("commissioning: AddTrustedRootCertificate", "certLen", len(rootCert))
	req, err := encodeOctetStringField(0, rootCert)
	if err != nil {
		return fmt.Errorf("encoding AddTrustedRootCertificate: %w", err)
	}
	slog.Debug("commissioning: AddTrustedRootCertificate encoded", "reqLen", len(req))
	_, err = c.Client.InvokeTimed(ctx, session, 0, 0x003E, 0x0B, req, 10000)
	if err != nil {
		return fmt.Errorf("invoking AddTrustedRootCertificate: %w", err)
	}
	return nil
}

func (c *Commissioner) addNOC(ctx context.Context, session Session, noc, icac, ipk []byte, adminSubject uint64) error {
	// AddNOC: cluster 0x003E, command 0x06.
	// This command requires a Timed Invoke per the Matter spec.
	req, encErr := encodeAddNOC(noc, icac, ipk, adminSubject)
	if encErr != nil {
		return fmt.Errorf("encoding AddNOC: %w", encErr)
	}
	resp, err := c.Client.InvokeTimed(ctx, session, 0, 0x003E, 0x06, req, 10000)
	if err != nil {
		return fmt.Errorf("invoking AddNOC: %w", err)
	}
	return checkNOCResponse(resp)
}

func (c *Commissioner) setupNetwork(ctx context.Context, session Session, creds *NetworkCredentials) error {
	switch creds.Type {
	case NetworkWiFi:
		// AddOrUpdateWiFiNetwork: cluster 0x0031, command 0x02.
		req, err := encodeWiFiNetwork(creds.WiFi.SSID, creds.WiFi.Password)
		if err != nil {
			return fmt.Errorf("encoding WiFi network: %w", err)
		}
		_, err = c.Client.Invoke(ctx, session, 0, 0x0031, 0x02, req)
		if err != nil {
			return fmt.Errorf("invoking AddOrUpdateWiFiNetwork: %w", err)
		}
	case NetworkThread:
		// AddOrUpdateThreadNetwork: cluster 0x0031, command 0x03.
		req, err := encodeOctetStringField(0, creds.Thread.OperationalDataset)
		if err != nil {
			return fmt.Errorf("encoding Thread network: %w", err)
		}
		_, err = c.Client.Invoke(ctx, session, 0, 0x0031, 0x03, req)
		if err != nil {
			return fmt.Errorf("invoking AddOrUpdateThreadNetwork: %w", err)
		}
	}
	return nil
}

func (c *Commissioner) connectNetwork(ctx context.Context, session Session, creds *NetworkCredentials) error {
	// ConnectNetwork: cluster 0x0031, command 0x06.
	var networkID []byte
	switch creds.Type {
	case NetworkWiFi:
		networkID = []byte(creds.WiFi.SSID)
	case NetworkThread:
		// For Thread, the network ID is the extended PAN ID from the dataset.
		// Simplified: use the first 8 bytes of the dataset.
		if len(creds.Thread.OperationalDataset) >= 8 {
			networkID = creds.Thread.OperationalDataset[:8]
		}
	}
	req, err := encodeOctetStringField(0, networkID)
	if err != nil {
		return fmt.Errorf("encoding ConnectNetwork: %w", err)
	}
	_, err = c.Client.Invoke(ctx, session, 0, 0x0031, 0x06, req)
	if err != nil {
		return fmt.Errorf("invoking ConnectNetwork: %w", err)
	}
	return nil
}

func (c *Commissioner) commissioningComplete(ctx context.Context, session Session) error {
	// CommissioningComplete: cluster 0x0030, command 0x04.
	resp, err := c.Client.Invoke(ctx, session, 0, 0x0030, 0x04, nil)
	if err != nil {
		return fmt.Errorf("invoking CommissioningComplete: %w", err)
	}
	return checkCommandErrorCode(resp, "CommissioningComplete")
}

// encodeArmFailsafe produces TLV-encoded fields for the ArmFailSafe command.
// Tag 0: ExpiryLengthSeconds (uint16), Tag 1: Breadcrumb (uint64).
func encodeArmFailsafe(seconds uint16) ([]byte, error) {
	w := tlv.NewWriter()
	if err := w.PutUnsignedInt(tlv.ContextTag(0), uint64(seconds)); err != nil {
		return nil, err
	}
	if err := w.PutUnsignedInt(tlv.ContextTag(1), 0); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

// encodeAddNOC produces TLV-encoded fields for the AddNOC command.
func encodeAddNOC(noc, icac, ipk []byte, adminSubject uint64) ([]byte, error) {
	w := tlv.NewWriter()
	if err := w.PutOctetString(tlv.ContextTag(0), noc); err != nil {
		return nil, err
	}
	if icac != nil {
		if err := w.PutOctetString(tlv.ContextTag(1), icac); err != nil {
			return nil, err
		}
	}
	if err := w.PutOctetString(tlv.ContextTag(2), ipk); err != nil {
		return nil, err
	}
	if err := w.PutUnsignedInt(tlv.ContextTag(3), adminSubject); err != nil {
		return nil, err
	}
	// AdminVendorId — use test vendor ID.
	if err := w.PutUnsignedInt(tlv.ContextTag(4), 0xFFF1); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

// encodeWiFiNetwork produces TLV-encoded fields for AddOrUpdateWiFiNetwork.
// Tag 0: SSID (octets), Tag 1: Credentials (octets).
func encodeWiFiNetwork(ssid, password string) ([]byte, error) {
	w := tlv.NewWriter()
	if err := w.PutOctetString(tlv.ContextTag(0), []byte(ssid)); err != nil {
		return nil, err
	}
	if err := w.PutOctetString(tlv.ContextTag(1), []byte(password)); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

// encodeOctetStringField produces a single TLV octet-string field at the given tag.
func encodeOctetStringField(tag uint8, data []byte) ([]byte, error) {
	w := tlv.NewWriter()
	if err := w.PutOctetString(tlv.ContextTag(tag), data); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

// encodeUintField produces a single TLV unsigned integer field at the given tag.
func encodeUintField(tag uint8, val uint64) ([]byte, error) {
	w := tlv.NewWriter()
	if err := w.PutUnsignedInt(tlv.ContextTag(tag), val); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

// checkNOCResponse parses the NOCResponse which has a different layout than
// other command responses: tag 0 = StatusCode, tag 1 = FabricIndex, tag 2 = DebugText.
func checkNOCResponse(resp []byte) error {
	if len(resp) == 0 {
		return nil
	}
	type nocResponse struct {
		StatusCode  uint8  `tlv:"0,uint"`
		FabricIndex uint8  `tlv:"1,uint"`
		DebugText   string `tlv:"2,utf8"`
	}
	var parsed nocResponse
	if err := tlv.Unmarshal(tlv.WrapStruct(resp), &parsed); err != nil {
		return nil
	}
	if parsed.StatusCode != 0 {
		msg := fmt.Sprintf("AddNOC returned error code %d", parsed.StatusCode)
		if parsed.DebugText != "" {
			msg += ": " + parsed.DebugText
		}
		return fmt.Errorf("%s", msg)
	}
	slog.Debug("commissioning: AddNOC succeeded", "fabricIndex", parsed.FabricIndex)
	return nil
}

// checkCommandErrorCode parses a command response's ErrorCode field (tag 0).
// Returns nil if the response is empty/nil or ErrorCode is 0 (success).
func checkCommandErrorCode(resp []byte, cmdName string) error {
	if len(resp) == 0 {
		return nil
	}
	// Parse the raw TLV fields: wrap in a struct for Unmarshal.
	type errorResponse struct {
		ErrorCode uint8  `tlv:"0,uint"`
		DebugText string `tlv:"1,utf8"`
	}
	var parsed errorResponse
	if err := tlv.Unmarshal(tlv.WrapStruct(resp), &parsed); err != nil {
		// If we can't parse, treat as success (may be an empty response).
		return nil
	}
	if parsed.ErrorCode != 0 {
		msg := fmt.Sprintf("%s returned error code %d", cmdName, parsed.ErrorCode)
		if parsed.DebugText != "" {
			msg += ": " + parsed.DebugText
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
