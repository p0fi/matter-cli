// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import (
	"context"
	"fmt"
	"testing"
)

// mockDiscoverer implements DeviceDiscoverer for testing.
type mockDiscoverer struct {
	addr string
	err  error
}

func (m *mockDiscoverer) DiscoverCommissionable(_ context.Context, _ uint16) (string, error) {
	return m.addr, m.err
}

// mockSession implements Session for testing.
type mockSession struct {
	closed bool
}

func (m *mockSession) Close() error {
	m.closed = true
	return nil
}

// mockSessionEstablisher implements SessionEstablisher for testing.
type mockSessionEstablisher struct {
	paseSession *mockSession
	caseSession *mockSession
	paseErr     error
	caseErr     error
}

func (m *mockSessionEstablisher) EstablishPASE(_ context.Context, _ string, _ uint32) (Session, error) {
	if m.paseErr != nil {
		return nil, m.paseErr
	}
	return m.paseSession, nil
}

func (m *mockSessionEstablisher) EstablishCASE(_ context.Context, _ string, _ uint64) (Session, error) {
	if m.caseErr != nil {
		return nil, m.caseErr
	}
	return m.caseSession, nil
}

// mockInteractionClient implements InteractionClient for testing.
type mockInteractionClient struct {
	invokeResp []byte
	invokeErr  error
	readResp   []byte
	readErr    error
}

func (m *mockInteractionClient) Invoke(_ context.Context, _ Session, _ uint16, _, _ uint32, _ []byte) ([]byte, error) {
	return m.invokeResp, m.invokeErr
}

func (m *mockInteractionClient) InvokeTimed(_ context.Context, _ Session, _ uint16, _, _ uint32, _ []byte, _ uint16) ([]byte, error) {
	return m.invokeResp, m.invokeErr
}

func (m *mockInteractionClient) ReadAttribute(_ context.Context, _ Session, _ uint16, _, _ uint32) ([]byte, error) {
	return m.readResp, m.readErr
}

// mockNOCIssuer implements NOCIssuer for testing.
type mockNOCIssuer struct {
	noc          []byte
	icac         []byte
	ipk          []byte
	rootCert     []byte
	adminSubject uint64
	err          error
}

func (m *mockNOCIssuer) IssueNOC(_ []byte, _ uint64) ([]byte, []byte, []byte, []byte, uint64, error) {
	return m.noc, m.icac, m.ipk, m.rootCert, m.adminSubject, m.err
}

// mockAttestationValidator implements AttestationValidator for testing.
type mockAttestationValidator struct {
	dacErr  error
	attErr  error
	devInfo *DeviceInfo
}

func (m *mockAttestationValidator) ValidateDAC(_ DACChain) (*DeviceInfo, error) {
	return m.devInfo, m.dacErr
}

func (m *mockAttestationValidator) ValidateAttestation(_ AttestationInfo, _ []byte, _ []byte) error {
	return m.attErr
}

func newTestCommissioner() *Commissioner {
	return &Commissioner{
		Discoverer: &mockDiscoverer{addr: "192.168.1.100:5540"},
		Sessions: &mockSessionEstablisher{
			paseSession: &mockSession{},
			caseSession: &mockSession{},
		},
		Client: &mockInteractionClient{
			invokeResp: nil, // nil signals success (no response data to parse)
			readResp:   []byte{1, 2, 3},
		},
		NOCIssuer: &mockNOCIssuer{
			noc:          []byte{0x30},
			icac:         []byte{0x30},
			ipk:          make([]byte, 16),
			rootCert:     []byte{0x30},
			adminSubject: 112233,
		},
		Attestation: &mockAttestationValidator{
			devInfo: &DeviceInfo{VendorID: 0xFFF1, ProductID: 0x8001},
		},
	}
}

func TestCommissioner_Commission_Success(t *testing.T) {
	c := newTestCommissioner()

	// Create a valid QR code for the setup code.
	payload := SetupPayload{
		Version:               0,
		VendorID:              0xFFF1,
		ProductID:             0x8001,
		CommissioningFlow:     FlowStandard,
		DiscoveryCapabilities: DiscoveryOnNetwork,
		Discriminator:         3840,
		Passcode:              20202021,
	}
	qr, err := payload.QRCode()
	if err != nil {
		t.Fatalf("QRCode(): %v", err)
	}

	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    1,
	}

	if _, err := c.Commission(context.Background(), params); err != nil {
		t.Fatalf("Commission: %v", err)
	}
}

func TestCommissioner_Commission_WithWiFi(t *testing.T) {
	c := newTestCommissioner()

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	wifiCreds := NewWiFiCredentials("TestNet", "TestPass")
	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    2,
		Network:   &wifiCreds,
	}

	if _, err := c.Commission(context.Background(), params); err != nil {
		t.Fatalf("Commission with WiFi: %v", err)
	}
}

func TestCommissioner_Commission_WithThread(t *testing.T) {
	c := newTestCommissioner()

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	threadCreds := NewThreadCredentials(make([]byte, 16))
	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    3,
		Network:   &threadCreds,
	}

	if _, err := c.Commission(context.Background(), params); err != nil {
		t.Fatalf("Commission with Thread: %v", err)
	}
}

func TestCommissioner_Commission_DiscoveryError(t *testing.T) {
	c := newTestCommissioner()
	c.Discoverer = &mockDiscoverer{err: fmt.Errorf("no device found")}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    1,
	}

	_, err := c.Commission(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for discovery failure")
	}
}

func TestCommissioner_Commission_PASEError(t *testing.T) {
	c := newTestCommissioner()
	c.Sessions = &mockSessionEstablisher{
		paseErr: fmt.Errorf("PASE failed"),
	}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    1,
	}

	_, err := c.Commission(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for PASE failure")
	}
}

func TestCommissioner_Commission_InvalidSetupCode(t *testing.T) {
	c := newTestCommissioner()

	params := CommissioningParams{
		SetupCode: "invalid",
		NodeID:    1,
	}

	_, err := c.Commission(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for invalid setup code")
	}
}

func TestCommissioner_Commission_ManualCode(t *testing.T) {
	c := newTestCommissioner()

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	code, _ := payload.ManualPairingCode()

	params := CommissioningParams{
		SetupCode: code,
		NodeID:    1,
	}

	if _, err := c.Commission(context.Background(), params); err != nil {
		t.Fatalf("Commission with manual code: %v", err)
	}
}

func TestCommissioner_ProgressCallback(t *testing.T) {
	c := newTestCommissioner()

	var steps []CommissioningStep
	c.OnProgress = func(step CommissioningStep) {
		steps = append(steps, step)
	}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    1,
	}

	if _, err := c.Commission(context.Background(), params); err != nil {
		t.Fatalf("Commission: %v", err)
	}

	if len(steps) == 0 {
		t.Error("expected progress callbacks")
	}

	// First step should be ParseSetupCode.
	if steps[0] != StepParseSetupCode {
		t.Errorf("first step: got %v, want %v", steps[0], StepParseSetupCode)
	}

	// Last step should be CommissioningComplete.
	if steps[len(steps)-1] != StepCommissioningComplete {
		t.Errorf("last step: got %v, want %v", steps[len(steps)-1], StepCommissioningComplete)
	}
}

func TestCommissioner_Commission_NOCIssuerError(t *testing.T) {
	c := newTestCommissioner()
	c.NOCIssuer = &mockNOCIssuer{err: fmt.Errorf("NOC generation failed")}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    1,
	}

	_, err := c.Commission(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for NOC issuer failure")
	}
}

func TestCommissioner_Commission_CASEError(t *testing.T) {
	c := newTestCommissioner()
	c.Sessions = &mockSessionEstablisher{
		paseSession: &mockSession{},
		caseErr:     fmt.Errorf("CASE failed"),
	}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    1,
	}

	_, err := c.Commission(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for CASE failure")
	}
}

func TestCommissioningStepString(t *testing.T) {
	tests := []struct {
		step CommissioningStep
		want string
	}{
		{StepParseSetupCode, "ParseSetupCode"},
		{StepDiscover, "Discover"},
		{StepEstablishPASE, "EstablishPASE"},
		{StepEstablishCASE, "EstablishCASE"},
		{CommissioningStep(99), "Step(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.step.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseSetupCode(t *testing.T) {
	// QR code.
	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	p, err := parseSetupCode(qr)
	if err != nil {
		t.Fatalf("parseSetupCode(QR): %v", err)
	}
	if p.Passcode != 20202021 {
		t.Errorf("QR passcode: got %d, want 20202021", p.Passcode)
	}

	// Manual code.
	manual, _ := payload.ManualPairingCode()
	p, err = parseSetupCode(manual)
	if err != nil {
		t.Fatalf("parseSetupCode(manual): %v", err)
	}
	if p.Passcode != 20202021 {
		t.Errorf("manual passcode: got %d, want 20202021", p.Passcode)
	}
}

func TestCommissioner_Commission_Cancelled(t *testing.T) {
	c := newTestCommissioner()
	c.Discoverer = &mockDiscoverer{err: context.Canceled}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    1,
	}

	_, err := c.Commission(ctx, params)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
