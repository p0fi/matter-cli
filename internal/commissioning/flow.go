// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/tlv"
	"github.com/p0fi/matter-cli/internal/transport"
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
	// OnNetwork indicates the device is already reachable over IP (e.g. mDNS
	// discovery or static address). When true, network provisioning steps are
	// skipped regardless of the device's reported NetworkCommissioning capabilities.
	OnNetwork bool
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
	StepReadCommissioningInfo
	StepReadBasicInfo
	StepAttestationRequest
	StepValidateAttestation
	StepCSRRequest
	StepGenerateNOC
	StepAddTrustedRoot
	StepAddNOC
	StepNetworkSetup
	StepNetworkConnect
	StepEstablishCASE
	StepCommissioningComplete
)

// String returns a human-readable name for the commissioning step.
func (s CommissioningStep) String() string {
	names := [...]string{
		"ParseSetupCode",
		"Discover",
		"EstablishPASE",
		"ArmFailsafe",
		"ReadCommissioningInfo",
		"ReadBasicInfo",
		"AttestationRequest",
		"ValidateAttestation",
		"CSRRequest",
		"GenerateNOC",
		"AddTrustedRoot",
		"AddNOC",
		"NetworkSetup",
		"NetworkConnect",
		"EstablishCASE",
		"CommissioningComplete",
	}
	if int(s) < len(names) {
		return names[s]
	}
	return fmt.Sprintf("Step(%d)", int(s))
}

// DeviceDiscoverer abstracts mDNS/DNS-SD device discovery.
type DeviceDiscoverer interface {
	// DiscoverCommissionable finds a commissionable device matching the given
	// discriminator. caps is the DiscoveryCapabilities bitmask from the setup
	// payload; implementations may use it to skip transports the device does
	// not support. A zero value means unknown — try all transports.
	DiscoverCommissionable(ctx context.Context, discriminator uint16, caps DiscoveryCapabilities) (addr string, err error)
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

	// RemoteAddress returns the address of the remote peer (e.g. "192.168.1.90:5540").
	RemoteAddress() string
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

	// BLEReconnectInitialWait overrides bleReconnectInitialWait when non-zero.
	// Used in tests to avoid sleeping for seconds.
	BLEReconnectInitialWait time.Duration
	// BLEReconnectRetryInterval overrides bleReconnectRetryInterval when non-zero.
	// Used in tests to avoid sleeping for seconds.
	BLEReconnectRetryInterval time.Duration
	// BLEReconnectMaxAttempts overrides bleReconnectMaxAttempts when > 0.
	// Used in tests to reduce the number of retries.
	BLEReconnectMaxAttempts int

	// CASEInitialWait overrides the initial wait before the first CASE attempt
	// when non-zero. Used in tests to avoid sleeping for seconds.
	CASEInitialWait time.Duration
	// CASERetryInterval overrides the 5-second between-attempt wait when non-zero.
	CASERetryInterval time.Duration
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
		params.FailsafeSeconds = 180
	}

	// Step 1: Determine passcode, discriminator, and discovery capabilities.
	var passcode uint32
	var discriminator uint16
	var caps DiscoveryCapabilities
	if params.Passcode != 0 {
		// Direct passcode provided — skip setup code parsing.
		// caps remains 0 (unknown): try all transports.
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
		caps = payload.DiscoveryCapabilities
		shortDisc := discriminator&0xFF == 0
		slog.Debug("commissioning: parsed setup code",
			"passcode", passcode,
			"discriminator", fmt.Sprintf("0x%03X (%d)", discriminator, discriminator),
			"shortDiscriminator", shortDisc,
			"vendorID", fmt.Sprintf("0x%04X", payload.VendorID),
			"productID", fmt.Sprintf("0x%04X", payload.ProductID),
			"flow", payload.CommissioningFlow,
			"discoveryCapabilities", fmt.Sprintf("0x%02X", caps),
		)
	}
	// Step 2: Discover device.
	c.reportProgress(StepDiscover)
	addr, err := c.Discoverer.DiscoverCommissionable(ctx, discriminator, caps)
	if err != nil {
		return nil, fmt.Errorf("commissioning: discovering device: %w", err)
	}

	// If the device was discovered via IP (not BLE), it is already on the
	// network. This overrides any device-reported NetworkCommissioning
	// capabilities that might incorrectly claim Thread/WiFi-only support.
	if !params.OnNetwork && !strings.HasPrefix(addr, "ble://") {
		params.OnNetwork = true
		slog.Debug("commissioning: device discovered via IP — marking as on-network")
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

	// Step 5: Read commissioning info.
	//
	// Read SupportsConcurrentConnection from the GeneralCommissioning
	// cluster (0x0030, attribute 0x0004). This attribute tells us whether
	// the device can maintain its commissioning transport (BLE) while
	// simultaneously joining its operational network. When false, the
	// device WILL drop BLE after ConnectNetwork — and may also drop it
	// prematurely during AddNOC on constrained MCUs.
	//
	// chip-tool reads this in kReadCommissioningInfo and adjusts its
	// entire commissioning strategy accordingly. We must do the same.
	c.reportProgress(StepReadCommissioningInfo)
	supportsConcurrentConnection := true // default per spec if attribute is missing
	if data, rdErr := c.Client.ReadAttribute(ctx, paseSession, 0, 0x0030, 0x0004); rdErr == nil {
		supportsConcurrentConnection = decodeTLVBool(data)
		slog.Debug("commissioning: read SupportsConcurrentConnection",
			"value", supportsConcurrentConnection)
	} else {
		slog.Debug("commissioning: failed to read SupportsConcurrentConnection, defaulting to true",
			"err", rdErr)
	}

	// Step 5b: Read BasicInformation (sanity check over PASE).
	c.reportProgress(StepReadBasicInfo)
	if _, err := c.Client.ReadAttribute(ctx, paseSession, 0, 0x0028, 0x0001); err != nil {
		return nil, fmt.Errorf("commissioning: reading basic information: %w", err)
	}

	// Check whether the device requires network credentials that were not
	// provided. Read the NetworkCommissioning cluster (0x0031) FeatureMap
	// attribute (0xFFFC) on endpoint 0 to discover the device's network
	// interfaces:
	//   Bit 0 (0x01): Wi-Fi
	//   Bit 1 (0x02): Thread
	//   Bit 2 (0x04): Ethernet
	//
	// If the device is already on-network and the user supplied credentials,
	// warn and discard them — network provisioning is unnecessary.
	if params.OnNetwork && params.Network != nil {
		slog.Warn("commissioning: device is already on-network; ignoring supplied network credentials",
			"networkType", params.Network.Type.String())
		params.Network = nil
	}

	// If the device only supports Thread (or only Wi-Fi) and no matching
	// credentials were supplied, bail out now with a clear error message
	// instead of proceeding to AddNOC where the BLE connection would drop
	// and leave the user with a cryptic timeout.
	if params.Network == nil && !params.OnNetwork {
		if featureData, rdErr := c.Client.ReadAttribute(ctx, paseSession, 0, 0x0031, 0xFFFC); rdErr == nil {
			features := decodeTLVUint32(featureData)
			hasWiFi := features&0x01 != 0
			hasThread := features&0x02 != 0
			hasEthernet := features&0x04 != 0

			slog.Debug("commissioning: network commissioning feature map",
				"features", fmt.Sprintf("0x%04X", features),
				"wifi", hasWiFi, "thread", hasThread, "ethernet", hasEthernet)

			if hasThread && !hasEthernet && !hasWiFi {
				return nil, fmt.Errorf("commissioning: device requires Thread network credentials\n" +
					"  Provide a Thread operational dataset with --thread-dataset <hex>")
			}
			if hasWiFi && !hasEthernet && !hasThread {
				return nil, fmt.Errorf("commissioning: device requires WiFi network credentials\n" +
					"  Provide WiFi credentials with --wifi-ssid <ssid> --wifi-password <password>")
			}
			if (hasThread || hasWiFi) && !hasEthernet {
				return nil, fmt.Errorf("commissioning: device requires network credentials (supports WiFi and Thread)\n" +
					"  Provide WiFi credentials with --wifi-ssid <ssid> --wifi-password <password>\n" +
					"  Or a Thread operational dataset with --thread-dataset <hex>")
			}
		}
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
	//
	// When SupportsConcurrentConnection is false, the device cannot
	// maintain BLE while processing AddNOC. BLE WILL drop here — this
	// is expected behaviour for non-concurrent devices, not a bug or a
	// slow-MCU artifact. The device deliberately sheds the BLE transport
	// because it cannot run both BLE and fabric credential processing
	// simultaneously.
	//
	// For concurrent devices, a BLE drop during AddNOC is unexpected but
	// still recoverable — treat it the same way (optimistic proceed).
	//
	// When the transport reports a connection-closed error here we treat
	// AddNOC as a *potential* success and proceed. If the device is
	// reachable on its operational network and CASE succeeds, AddNOC
	// clearly worked. If not, the failsafe timer will roll back the
	// device's state automatically.
	c.reportProgress(StepAddNOC)
	addNOCErr := c.addNOC(ctx, paseSession, noc, icac, ipk, adminSubject)
	bleDropped := false
	if addNOCErr != nil {
		if errors.Is(addNOCErr, transport.ErrConnClosed) || errors.Is(addNOCErr, context.DeadlineExceeded) || errors.Is(addNOCErr, protocol.ErrExchangeClosed) {
			// The request was fully sent but we never got the response
			// because the transport died.
			if !supportsConcurrentConnection {
				slog.Debug("commissioning: BLE disconnected during AddNOC — expected for non-concurrent device (SupportsConcurrentConnection=false)",
					"err", addNOCErr)
			} else {
				slog.Warn("commissioning: BLE disconnected during AddNOC on a concurrent device — unexpected but proceeding optimistically",
					"err", addNOCErr)
			}
			bleDropped = true
		} else {
			return nil, fmt.Errorf("commissioning: adding NOC: %w", addNOCErr)
		}
	}

	// Step 12-13: Network setup + ConnectNetwork (if needed).
	//
	// Network provisioning happens AFTER AddNOC. The device needs its
	// operational certificate before it can operate on the network. The
	// sequence is:
	//   1. AddOrUpdateThreadNetwork / AddOrUpdateWiFiNetwork — store creds
	//   2. ConnectNetwork — tell device to join the network
	//
	// Per Matter Core Spec §5.5.2 ("Non-Concurrent Connection
	// Commissioning Flow"), a device with SupportsConcurrentConnection=false
	// cannot maintain its commissioning transport (BLE) while joining the
	// operational network. It SHALL close all commissioning channels after
	// ConnectNetwork. For BLE-commissioned devices this means BLE will
	// drop after ConnectNetwork — this is required behaviour, not a bug.
	//
	// On non-concurrent devices BLE may also drop earlier, during AddNOC,
	// because the device cannot run both BLE and credential processing at
	// the same time. This is NOT a slow-MCU artifact — it is the device
	// deliberately shedding BLE because SupportsConcurrentConnection=false.
	//
	// We handle both cases optimistically: once BLE drops we proceed to
	// CASE discovery. If the device successfully joined its network it
	// will advertise on _matter._tcp; if not, the failsafe timer rolls
	// back its state automatically.
	//
	// Special case: if BLE dropped during AddNOC and we still have
	// network credentials to deliver, we must re-establish PASE over BLE.
	// The device is still in its failsafe window and will re-advertise on
	// BLE within a few seconds. Without the Thread/WiFi dataset the
	// device cannot join any IP network, so skipping network setup would
	// guarantee CASE failure.
	needsNetwork := params.Network != nil && params.Network.Type != NetworkEthernet

	if needsNetwork && bleDropped {
		// Re-connect BLE and deliver credentials over a fresh PASE session.
		// BLE dropped during AddNOC before we could send network credentials.
		// For non-concurrent devices (SupportsConcurrentConnection=false) this
		// is expected — the device cannot maintain BLE during credential
		// processing. It will re-advertise on BLE within a few seconds once
		// AddNOC completes and its BLE stack re-initialises.
		slog.Debug("commissioning: BLE dropped during AddNOC with pending network credentials — attempting BLE reconnect to deliver credentials",
			"addr", addr, "networkType", params.Network.Type.String(),
			"supportsConcurrentConnection", supportsConcurrentConnection)

		reconnectSession, reconnectErr := c.reconnectPASEAfterAddNOC(ctx, addr, passcode)
		if reconnectErr != nil {
			// We could not reconnect BLE. This is a hard failure for
			// Thread/WiFi devices: they have a NOC but no network
			// credentials, so they cannot join the operational network.
			// The failsafe will roll back their state automatically.
			return nil, fmt.Errorf(
				"commissioning: BLE dropped during AddNOC and reconnect failed — "+
					"device cannot join %s network without credentials "+
					"(failsafe will roll back the device): %w",
				params.Network.Type.String(), reconnectErr)
		}
		defer reconnectSession.Close()

		slog.Debug("commissioning: BLE reconnected after AddNOC, delivering network credentials")
		// Treat reconnect session as paseSession for network provisioning below.
		paseSession = reconnectSession
		bleDropped = false

		// Re-arm the failsafe timer. The original timer may have nearly expired
		// (or already expired) during the BLE reconnect, which can take 30-60 s
		// on constrained devices. We use the spec-maximum (900 s) rather than
		// params.FailsafeSeconds here because after reconnect we still need to:
		//   1. Deliver network credentials (seconds)
		//   2. Wait for the device to join its operational network (up to 60 s)
		//   3. Discover the device via mDNS (WatchOperational, up to 3 min)
		//   4. Complete CASE and send CommissioningComplete
		// 180 s is too tight — the failsafe was expiring right as mDNS
		// discovery reached its 3-minute limit.
		const bleReconnectFailsafeSeconds = 900
		slog.Debug("commissioning: re-arming failsafe after BLE reconnect",
			"seconds", bleReconnectFailsafeSeconds)
		if err := c.armFailsafe(ctx, paseSession, bleReconnectFailsafeSeconds); err != nil {
			reconnectSession.Close()
			return nil, fmt.Errorf("commissioning: re-arming failsafe after BLE reconnect: %w", err)
		}
	}

	if needsNetwork && !bleDropped {
		c.reportProgress(StepNetworkSetup)
		if err := c.setupNetwork(ctx, paseSession, params.Network); err != nil {
			if errors.Is(err, transport.ErrConnClosed) || errors.Is(err, protocol.ErrExchangeClosed) {
				slog.Debug("commissioning: BLE disconnected during NetworkSetup, proceeding optimistically",
					"err", err)
				bleDropped = true
			} else {
				return nil, fmt.Errorf("commissioning: setting up network: %w", err)
			}
		}

		if !bleDropped {
			c.reportProgress(StepNetworkConnect)
			connectErr := c.connectNetwork(ctx, paseSession, params.Network)
			if connectErr != nil {
				if errors.Is(connectErr, transport.ErrConnClosed) || errors.Is(connectErr, context.DeadlineExceeded) || errors.Is(connectErr, protocol.ErrExchangeClosed) {
					slog.Debug("commissioning: BLE disconnected during ConnectNetwork, proceeding optimistically",
						"err", connectErr)
					bleDropped = true
				} else {
					return nil, fmt.Errorf("commissioning: connecting network: %w", connectErr)
				}
			}
		}
	}

	// Step 14: Establish CASE session.
	// CommissioningComplete must be sent over a CASE session (not PASE).
	// After AddNOC + ConnectNetwork the device should be on its
	// operational network and ready to accept CASE connections.
	c.reportProgress(StepEstablishCASE)

	// After network provisioning the device needs time to join its
	// operational network. Thread devices in particular must attach to
	// the Thread mesh, obtain an IP address, and start advertising on
	// _matter._tcp — this can take 5-120 seconds on constrained hardware.
	//
	// When BLE dropped (typical for Thread/WiFi commissioning), the
	// EstablishCASE implementation uses WatchOperational — a single
	// continuous mDNS browse that blocks until the device appears or the
	// timeout expires. Because WatchOperational already handles all the
	// "wait for the device to come online" time internally, we only need
	// a single attempt here: the long browse window IS the retry window.
	//
	// For IP commissioning (no BLE drop), keep the original short-retry
	// behaviour: the device should already be online and CASE failures
	// are most likely transient network glitches worth retrying quickly.
	caseRetries := 3
	initialWait := 2 * time.Second
	retryWait := 5 * time.Second
	if bleDropped {
		// One attempt is enough — WatchOperational inside EstablishCASE
		// keeps the mDNS socket open for up to 3 minutes and returns as
		// soon as the device's _matter._tcp announcement arrives.
		caseRetries = 1
		initialWait = 0
		slog.Debug("commissioning: BLE dropped, using single-attempt continuous mDNS watch for CASE")
	}
	if c.CASEInitialWait != 0 {
		initialWait = c.CASEInitialWait
	}
	if c.CASERetryInterval != 0 {
		retryWait = c.CASERetryInterval
	}

	var caseSession Session
	var caseErr error
	for attempt := 0; attempt < caseRetries; attempt++ {
		if attempt > 0 {
			slog.Debug("commissioning: retrying CASE", "attempt", attempt+1)
		}
		// Wait before each attempt to give the device time to transition.
		wait := initialWait
		if attempt > 0 {
			wait = retryWait
		}
		select {
		case <-time.After(wait):
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
		if bleDropped {
			return nil, fmt.Errorf("commissioning: BLE disconnected during commissioning and device not reachable on operational network: %w", caseErr)
		}
		return nil, fmt.Errorf("commissioning: establishing CASE session: %w", caseErr)
	}
	defer caseSession.Close()

	// Step 15: CommissioningComplete over the CASE session.
	c.reportProgress(StepCommissioningComplete)
	if err := c.commissioningComplete(ctx, caseSession); err != nil {
		return nil, fmt.Errorf("commissioning: completing commissioning: %w", err)
	}

	// Read device info over the CASE session now that commissioning is complete.
	// Use the CASE session's remote address (IP:port) rather than the original
	// addr, which may be a BLE address from the discovery step.
	resultAddr := addr
	if ra := caseSession.RemoteAddress(); ra != "" {
		resultAddr = ra
	}
	result := &CommissioningResult{Address: resultAddr}
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

// decodeTLVUint32 extracts a uint32 from a raw TLV element.
func decodeTLVUint32(data []byte) uint32 {
	if len(data) == 0 {
		return 0
	}
	r := tlv.NewReader(bytes.NewReader(data))
	if err := r.Next(); err != nil {
		return 0
	}
	if v, ok := r.Value().(uint64); ok {
		return uint32(v)
	}
	return 0
}

// decodeTLVBool extracts a boolean from a raw TLV element. Returns false if the
// data is empty, not a boolean, or cannot be parsed.
func decodeTLVBool(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	r := tlv.NewReader(bytes.NewReader(data))
	if err := r.Next(); err != nil {
		return false
	}
	if v, ok := r.Value().(bool); ok {
		return v
	}
	return false
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
		resp, err := c.Client.Invoke(ctx, session, 0, 0x0031, 0x02, req)
		if err != nil {
			return fmt.Errorf("invoking AddOrUpdateWiFiNetwork: %w", err)
		}
		if err := checkNetworkResponse(resp, "AddOrUpdateWiFiNetwork"); err != nil {
			return err
		}
	case NetworkThread:
		// AddOrUpdateThreadNetwork: cluster 0x0031, command 0x03.
		req, err := encodeOctetStringField(0, creds.Thread.OperationalDataset)
		if err != nil {
			return fmt.Errorf("encoding Thread network: %w", err)
		}
		resp, err := c.Client.Invoke(ctx, session, 0, 0x0031, 0x03, req)
		if err != nil {
			return fmt.Errorf("invoking AddOrUpdateThreadNetwork: %w", err)
		}
		if err := checkNetworkResponse(resp, "AddOrUpdateThreadNetwork"); err != nil {
			return err
		}
	}
	return nil
}

// connectNetworkTimeout is the maximum time to wait for a ConnectNetwork
// response. Thread devices must attach to the mesh, obtain an IP address,
// and potentially negotiate Thread security — this routinely takes 30-60 s
// on constrained hardware. The default 30 s invoke timeout is not enough.
const connectNetworkTimeout = 120 * time.Second

func (c *Commissioner) connectNetwork(ctx context.Context, session Session, creds *NetworkCredentials) error {
	// ConnectNetwork: cluster 0x0031, command 0x06.
	var networkID []byte
	switch creds.Type {
	case NetworkWiFi:
		networkID = []byte(creds.WiFi.SSID)
	case NetworkThread:
		// For Thread, the network ID is the Extended PAN ID extracted from
		// the Thread operational dataset TLV (type 0x02, 8 bytes).
		extPanID, err := ExtractExtendedPANID(creds.Thread.OperationalDataset)
		if err != nil {
			return fmt.Errorf("extracting Extended PAN ID for ConnectNetwork: %w", err)
		}
		networkID = extPanID
	}
	slog.Debug("commissioning: ConnectNetwork",
		"networkID", fmt.Sprintf("%x", networkID),
		"networkType", creds.Type.String())
	req, err := encodeOctetStringField(0, networkID)
	if err != nil {
		return fmt.Errorf("encoding ConnectNetwork: %w", err)
	}
	// Use a longer invoke response timeout: the device must join the
	// network (Thread mesh attach, WiFi association) before it can reply,
	// which routinely exceeds the default 30 s timeout.
	invokeCtx := interaction.WithInvokeTimeout(ctx, connectNetworkTimeout)
	resp, err := c.Client.Invoke(invokeCtx, session, 0, 0x0031, 0x06, req)
	if err != nil {
		return fmt.Errorf("invoking ConnectNetwork: %w", err)
	}
	// ConnectNetworkResponse (command 0x07) contains:
	//   Field 0: NetworkingStatus (uint8) — 0 = Success
	//   Field 1: DebugText (optional UTF-8 string)
	//   Field 2: ErrorValue (optional int32)
	if err := checkNetworkResponse(resp, "ConnectNetwork"); err != nil {
		return err
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
// bleReconnectInitialWait is how long to wait after the premature AddNOC BLE
// drop before the first reconnect attempt. The device needs time to finish
// processing AddNOC (crypto validation, NVM write) and re-initialise its BLE
// stack before it re-advertises in its commissioning window.
const bleReconnectInitialWait = 3 * time.Second

// bleReconnectMaxAttempts is the maximum number of PASE re-establishment
// attempts after an AddNOC-triggered BLE drop.
const bleReconnectMaxAttempts = 6

// bleReconnectRetryInterval is the delay between reconnect attempts.
const bleReconnectRetryInterval = 4 * time.Second

// reconnectPASEAfterAddNOC attempts to re-establish a PASE session over BLE
// after the BLE connection dropped prematurely during AddNOC processing. This
// is necessary for Thread and WiFi devices that cannot join any IP network
// without first receiving their network credentials via the
// NetworkCommissioning cluster.
//
// The BLE drop here is NOT the spec-mandated closure (which happens after
// ConnectNetwork per §5.5.2). It is an implementation artifact: cert chain
// validation + NVM write on a single-core MCU can stall the BLE stack's
// heartbeat long enough for the central's supervision timeout to expire.
//
// After the drop the device continues processing AddNOC. Once its NVM write
// completes it re-initialises its BLE stack and re-opens its commissioning
// window (the failsafe timer is still running). It re-advertises with the
// same address (CoreBluetooth UUID on macOS, MAC on Linux) and the same
// discriminator. We use EstablishPASE with the original addr and passcode to
// reconnect.
func (c *Commissioner) reconnectPASEAfterAddNOC(ctx context.Context, addr string, passcode uint32) (Session, error) {
	initialWait := bleReconnectInitialWait
	if c.BLEReconnectInitialWait != 0 {
		initialWait = c.BLEReconnectInitialWait
	}
	retryInterval := bleReconnectRetryInterval
	if c.BLEReconnectRetryInterval != 0 {
		retryInterval = c.BLEReconnectRetryInterval
	}
	maxAttempts := bleReconnectMaxAttempts
	if c.BLEReconnectMaxAttempts > 0 {
		maxAttempts = c.BLEReconnectMaxAttempts
	}

	slog.Debug("commissioning: waiting for device to re-advertise after AddNOC reboot",
		"initialWait", initialWait)

	select {
	case <-time.After(initialWait):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		slog.Debug("commissioning: BLE reconnect attempt", "attempt", attempt, "maxAttempts", maxAttempts, "addr", addr)

		session, err := c.Sessions.EstablishPASE(ctx, addr, passcode)
		if err == nil {
			slog.Debug("commissioning: BLE reconnect succeeded", "attempt", attempt)
			return session, nil
		}
		lastErr = err
		slog.Debug("commissioning: BLE reconnect attempt failed", "attempt", attempt, "err", err)

		if attempt < maxAttempts {
			select {
			case <-time.After(retryInterval):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	return nil, fmt.Errorf("BLE reconnect after AddNOC failed after %d attempts: %w", maxAttempts, lastErr)
}

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

// checkNetworkResponse validates the NetworkConfigResponse / ConnectNetworkResponse
// returned by NetworkCommissioning cluster commands. The response TLV contains:
//
//	Field 0: NetworkingStatus (uint8) — 0 = Success
//	Field 1: DebugText (optional UTF-8 string)
//	Field 2: ErrorValue (optional int32)
func checkNetworkResponse(resp []byte, commandName string) error {
	if len(resp) == 0 {
		slog.Debug("commissioning: network command returned empty response (assuming success)",
			"command", commandName)
		return nil
	}

	// Parse the response TLV to extract NetworkingStatus.
	r := tlv.NewReader(bytes.NewReader(resp))
	var status uint64
	var debugText string
	var hasStatus bool

	for {
		if err := r.Next(); err != nil {
			break
		}
		elem := r.Element()
		if elem.Tag.Type != tlv.TagContextSpecific {
			continue
		}
		switch elem.Tag.TagNum {
		case 0: // NetworkingStatus
			if v, ok := elem.Value.(uint64); ok {
				status = v
				hasStatus = true
			}
		case 1: // DebugText
			if v, ok := elem.Value.(string); ok {
				debugText = v
			}
		case 2: // ErrorValue
			slog.Debug("commissioning: network command error value",
				"command", commandName, "errorValue", elem.Value)
		}
	}

	if !hasStatus {
		slog.Debug("commissioning: network command response has no NetworkingStatus field",
			"command", commandName, "rawLen", len(resp))
		return nil
	}

	slog.Debug("commissioning: network command response",
		"command", commandName,
		"networkingStatus", status,
		"debugText", debugText)

	// NetworkingStatus 0 = Success per Matter spec section 11.8.5.3.
	if status != 0 {
		statusName := networkingStatusName(status)
		if debugText != "" {
			return fmt.Errorf("%s failed: %s (status %d): %s", commandName, statusName, status, debugText)
		}
		return fmt.Errorf("%s failed: %s (status %d)", commandName, statusName, status)
	}
	return nil
}

// networkingStatusName returns a human-readable name for a NetworkingStatus value
// per Matter spec section 11.8.5.3.
func networkingStatusName(status uint64) string {
	switch status {
	case 0:
		return "Success"
	case 1:
		return "OutOfRange"
	case 2:
		return "BoundsExceeded"
	case 3:
		return "NetworkIDNotFound"
	case 4:
		return "DuplicateNetworkID"
	case 5:
		return "NetworkNotFound"
	case 6:
		return "RegulatoryError"
	case 7:
		return "AuthFailure"
	case 8:
		return "UnsupportedSecurity"
	case 9:
		return "OtherConnectionFailure"
	case 10:
		return "IPV6Failed"
	case 11:
		return "IPBindFailed"
	case 12:
		return "UnknownError"
	default:
		return fmt.Sprintf("Unknown(%d)", status)
	}
}

// checkCommandErrorCode parses a command response's ErrorCode field (tag 0).
// Returns nil if the response is empty/nil or ErrorCode is 0 (success).
func checkCommandErrorCode(resp []byte, commandName string) error {
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
		msg := fmt.Sprintf("%s returned error code %d", commandName, parsed.ErrorCode)
		if parsed.DebugText != "" {
			msg += ": " + parsed.DebugText
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
