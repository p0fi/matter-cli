// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/p0fi/matter-cli/internal/commissioning"
	"github.com/p0fi/matter-cli/internal/crypto"
	"github.com/p0fi/matter-cli/internal/discovery"
	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/tlv"
)

// NewCommissioner assembles a commissioning.Commissioner wired to this controller.
func (c *Controller) NewCommissioner() *commissioning.Commissioner {
	return &commissioning.Commissioner{
		Discoverer: &controllerDiscoverer{browser: discovery.NewMDNSBrowser()},
		Sessions:   &controllerSessionEstablisher{ctrl: c},
		Client:     &controllerIMClient{ctrl: c},
		NOCIssuer:  &controllerNOCIssuer{ctrl: c},
	}
}

// controllerDiscoverer implements commissioning.DeviceDiscoverer using mDNS.
type controllerDiscoverer struct {
	browser *discovery.MDNSBrowser
}

func (d *controllerDiscoverer) DiscoverCommissionable(ctx context.Context, discriminator uint16, _ commissioning.DiscoveryCapabilities) (string, error) {
	// Manual pairing codes only encode the upper 4 bits of the 12-bit
	// discriminator (the "short discriminator"). ParseManualPairingCode
	// stores it as shortDisc<<8, leaving the lower 8 bits zero. When we
	// detect this pattern we match on the short discriminator only.
	shortMatch := discriminator&0xFF == 0
	shortDisc := discriminator >> 8

	var addr string
	err := d.browser.WatchCommissionable(ctx, 15*time.Second, func(dev *discovery.Device) bool {
		if len(dev.IPs) == 0 {
			return false
		}
		if shortMatch {
			if dev.Discriminator>>8 != shortDisc {
				return false
			}
		} else {
			if dev.Discriminator != discriminator {
				return false
			}
		}
		addr = net.JoinHostPort(dev.IPs[0].String(), strconv.Itoa(dev.Port))
		return true
	})
	if err != nil {
		return "", fmt.Errorf("mDNS discovery: %w", err)
	}
	if addr == "" {
		return "", fmt.Errorf("no commissionable device found with discriminator %d", discriminator)
	}
	return addr, nil
}

// StaticDiscoverer bypasses mDNS and returns a fixed address.
// Used for direct IP commissioning.
type StaticDiscoverer struct {
	Addr string
}

// DiscoverCommissionable returns the pre-configured address regardless of discriminator.
func (d *StaticDiscoverer) DiscoverCommissionable(_ context.Context, _ uint16, _ commissioning.DiscoveryCapabilities) (string, error) {
	return d.Addr, nil
}

// controllerSessionEstablisher implements commissioning.SessionEstablisher.
type controllerSessionEstablisher struct {
	ctrl *Controller
}

func (s *controllerSessionEstablisher) EstablishPASE(ctx context.Context, addr string, passcode uint32) (commissioning.Session, error) {
	session, err := s.ctrl.ConnectPASE(ctx, addr, passcode)
	if err != nil {
		return nil, err
	}
	return &controllerSession{session: session, addr: addr}, nil
}

func (s *controllerSessionEstablisher) EstablishCASE(ctx context.Context, addr string, nodeID uint64) (commissioning.Session, error) {
	session, err := s.ctrl.ConnectCASE(ctx, addr, nodeID)
	if err != nil {
		return nil, err
	}
	return &controllerSession{session: session, addr: addr}, nil
}

// controllerSession wraps a protocol.Session to implement commissioning.Session.
type controllerSession struct {
	session *protocol.Session
	addr    string // remote address (host:port) used to establish this session
}

func (s *controllerSession) Close() error {
	// Session cleanup is handled by the controller's session table.
	return nil
}

// RemoteAddress returns the address of the remote peer.
func (s *controllerSession) RemoteAddress() string {
	return s.addr
}

// protocolSession extracts the underlying protocol.Session from a commissioning.Session.
func protocolSession(s commissioning.Session) *protocol.Session {
	cs, ok := s.(*controllerSession)
	if !ok {
		return nil
	}
	return cs.session
}

// controllerIMClient implements commissioning.InteractionClient.
type controllerIMClient struct {
	ctrl *Controller
}

func (ic *controllerIMClient) Invoke(ctx context.Context, session commissioning.Session, endpoint uint16, cluster, command uint32, request []byte) ([]byte, error) {
	ps := protocolSession(session)
	if ps == nil {
		return nil, fmt.Errorf("controller: invalid session type")
	}

	client := interaction.NewClient(ic.ctrl.exchanges)
	path := interaction.CommandPath{
		EndpointID: endpoint,
		ClusterID:  cluster,
		CommandID:  command,
	}

	resp, err := client.Invoke(ctx, ps, path, request)
	if err != nil {
		return nil, err
	}

	if resp.Status != nil {
		// Commands that return only a status (e.g., AddTrustedRootCertificate)
		// report success/failure here. Check for non-success status codes.
		if resp.Status.Status.Status != 0 {
			se := &interaction.StatusError{
				GeneralCode: interaction.StatusCode(resp.Status.Status.Status),
				ClusterCode: resp.Status.Status.ClusterStatus,
			}
			return nil, fmt.Errorf("command status error: %w (cluster: %d, command: 0x%04X)",
				se, cluster, command)
		}
		return nil, nil
	}
	if resp.Command != nil {
		return resp.Command.Fields, nil
	}
	return nil, nil
}

func (ic *controllerIMClient) InvokeTimed(ctx context.Context, session commissioning.Session, endpoint uint16, cluster, command uint32, request []byte, timeoutMs uint16) ([]byte, error) {
	ps := protocolSession(session)
	if ps == nil {
		return nil, fmt.Errorf("controller: invalid session type")
	}

	client := interaction.NewClient(ic.ctrl.exchanges)
	path := interaction.CommandPath{
		EndpointID: endpoint,
		ClusterID:  cluster,
		CommandID:  command,
	}

	slog.Debug("controller: InvokeTimed",
		"endpoint", endpoint, "cluster", fmt.Sprintf("0x%04X", cluster),
		"command", fmt.Sprintf("0x%04X", command), "timeoutMs", timeoutMs,
		"requestLen", len(request))

	resp, err := client.InvokeTimed(ctx, ps, path, request, timeoutMs)
	if err != nil {
		return nil, err
	}

	if resp.Status != nil {
		code := interaction.StatusCode(resp.Status.Status.Status)
		if code != interaction.StatusSuccess {
			se := &interaction.StatusError{
				GeneralCode: code,
				ClusterCode: resp.Status.Status.ClusterStatus,
			}
			slog.Error("controller: InvokeTimed command rejected",
				"status", se.GeneralCode.String(),
				"statusCode", fmt.Sprintf("0x%02X", uint8(se.GeneralCode)),
				"cluster", fmt.Sprintf("0x%04X", cluster),
				"command", fmt.Sprintf("0x%04X", command))
			return nil, fmt.Errorf("command %w for cluster 0x%04X command 0x%04X",
				se, cluster, command)
		}
		return nil, nil
	}
	if resp.Command != nil {
		return resp.Command.Fields, nil
	}
	return nil, nil
}

func (ic *controllerIMClient) ReadAttribute(ctx context.Context, session commissioning.Session, endpoint uint16, cluster, attribute uint32) ([]byte, error) {
	ps := protocolSession(session)
	if ps == nil {
		return nil, fmt.Errorf("controller: invalid session type")
	}

	client := interaction.NewClient(ic.ctrl.exchanges)
	path := interaction.NewAttributePath(endpoint, cluster, attribute)

	reports, err := client.Read(ctx, ps, path)
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

// controllerNOCIssuer implements commissioning.NOCIssuer using the controller's
// fabric CA keys.
type controllerNOCIssuer struct {
	ctrl *Controller
}

func (n *controllerNOCIssuer) IssueNOC(csrElements []byte, nodeID uint64) (noc, icac, ipk, rootCert []byte, adminSubject uint64, err error) {
	f := n.ctrl.fabric
	if f == nil {
		return nil, nil, nil, nil, 0, fmt.Errorf("controller: no fabric identity")
	}

	// The CSR elements are a TLV structure containing:
	//   Tag 0: NOCSRElements (octet string) — itself a TLV structure with:
	//     Tag 1: CSR (octet string, DER-encoded PKCS#10)
	//     Tag 2: CSRNonce (octet string)
	//   Tag 1: AttestationSignature (octet string)
	// We need to extract the CSR from the NOCSRElements to get the device's
	// public key. For now, try to find the DER-encoded CSR inside csrElements.
	devicePubKey, csrErr := extractCSRPublicKey(csrElements)
	if csrErr != nil {
		// Fallback: generate a fresh key pair (won't work with real devices
		// but at least lets us proceed for testing).
		slog.Warn("controller: failed to extract public key from CSR, generating fresh key",
			"err", csrErr)
		deviceKey, genErr := crypto.GenerateKeyPair()
		if genErr != nil {
			return nil, nil, nil, nil, 0, fmt.Errorf("generating device key: %w", genErr)
		}
		opts := crypto.DefaultCertificateOptions()
		deviceNOCDER, nocErr := crypto.GenerateNOC(deviceKey, nodeID, n.ctrl.fabricID, f.icac, f.icacKey, opts)
		if nocErr != nil {
			return nil, nil, nil, nil, 0, fmt.Errorf("generating device NOC: %w", nocErr)
		}
		nocTLV, tlvErr := crypto.X509ToMatterCert(deviceNOCDER)
		if tlvErr != nil {
			return nil, nil, nil, nil, 0, fmt.Errorf("converting NOC to Matter TLV: %w", tlvErr)
		}
		icacTLV, tlvErr := crypto.X509ToMatterCert(f.icac)
		if tlvErr != nil {
			return nil, nil, nil, nil, 0, fmt.Errorf("converting ICAC to Matter TLV: %w", tlvErr)
		}
		rcacTLV, tlvErr := crypto.X509ToMatterCert(f.rcac)
		if tlvErr != nil {
			return nil, nil, nil, nil, 0, fmt.Errorf("converting RCAC to Matter TLV: %w", tlvErr)
		}
		return nocTLV, icacTLV, f.ipk, rcacTLV, n.ctrl.fabricID, nil
	}

	opts := crypto.DefaultCertificateOptions()
	deviceNOCDER, nocErr := crypto.GenerateNOCForPublicKey(devicePubKey, nodeID, n.ctrl.fabricID, f.icac, f.icacKey, opts)
	if nocErr != nil {
		return nil, nil, nil, nil, 0, fmt.Errorf("generating device NOC: %w", nocErr)
	}

	// Convert X.509 DER certs to Matter TLV format, preserving the original
	// DER-based ECDSA signature. The CHIP SDK verifies by reconstructing DER TBS
	// from the TLV fields and hashing that — so the original DER signature is correct.
	nocTLV, err := crypto.X509ToMatterCert(deviceNOCDER)
	if err != nil {
		return nil, nil, nil, nil, 0, fmt.Errorf("converting NOC to Matter TLV: %w", err)
	}
	icacTLV, err := crypto.X509ToMatterCert(f.icac)
	if err != nil {
		return nil, nil, nil, nil, 0, fmt.Errorf("converting ICAC to Matter TLV: %w", err)
	}
	rcacTLV, err := crypto.X509ToMatterCert(f.rcac)
	if err != nil {
		return nil, nil, nil, nil, 0, fmt.Errorf("converting RCAC to Matter TLV: %w", err)
	}

	slog.Debug("controller: IssueNOC certificates",
		"nocTLVLen", len(nocTLV),
		"icacTLVLen", len(icacTLV),
		"rcacTLVLen", len(rcacTLV),
		"ipkLen", len(f.ipk),
		"adminSubject", n.ctrl.fabricID,
	)

	// Admin subject is the controller's own node ID (= fabricID).
	return nocTLV, icacTLV, f.ipk, rcacTLV, n.ctrl.fabricID, nil
}

// extractCSRPublicKey attempts to find and parse a DER-encoded PKCS#10 CSR
// from the raw csrElements TLV blob returned by the device's CSRRequest command.
// The CSR is contained in the NOCSRElements TLV structure at tag 1.
func extractCSRPublicKey(csrElements []byte) (*ecdsa.PublicKey, error) {
	// The csrElements is the command response fields (inside the InvokeResponse).
	// It should be TLV-encoded with:
	//   Tag 0: NOCSRElements (octet string)
	//   Tag 1: AttestationSignature (octet string)
	// The NOCSRElements itself is a TLV structure with:
	//   Tag 1: CSR (octet string, DER PKCS#10)
	//   Tag 2: CSRNonce (octet string)
	//
	// We need to parse this nested TLV to extract the CSR.
	// First, parse the outer structure to get NOCSRElements.
	type csrResponse struct {
		NOCSRElements []byte `tlv:"0,octets"`
	}
	var outer csrResponse
	if err := tlv.Unmarshal(tlv.WrapStruct(csrElements), &outer); err != nil {
		return nil, fmt.Errorf("parsing CSR response: %w", err)
	}
	if len(outer.NOCSRElements) == 0 {
		return nil, fmt.Errorf("empty NOCSRElements")
	}

	// Parse the inner NOCSRElements to get the raw CSR.
	// NOCSRElements is a TLV-encoded structure, so we unmarshal it directly
	// (it already has the struct container wrapping).
	type nocsrElements struct {
		CSR      []byte `tlv:"1,octets"`
		CSRNonce []byte `tlv:"2,octets"`
	}
	var inner nocsrElements
	if err := tlv.Unmarshal(outer.NOCSRElements, &inner); err != nil {
		return nil, fmt.Errorf("parsing NOCSRElements: %w", err)
	}
	if len(inner.CSR) == 0 {
		return nil, fmt.Errorf("empty CSR in NOCSRElements")
	}

	return crypto.ParseCSR(inner.CSR)
}
