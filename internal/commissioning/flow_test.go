// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/p0fi/matter-cli/internal/tlv"
	"github.com/p0fi/matter-cli/internal/transport"
)

// testThreadDataset builds a minimal but valid Thread Active Operational Dataset
// for testing. It contains all required TLV fields with dummy values so that
// ValidateThreadDataset accepts it.
func testThreadDataset() []byte {
	var ds []byte
	// Channel (type 0x00): 3 bytes value (page + channel)
	ds = append(ds, 0x00, 0x03, 0x00, 0x00, 0x0F)
	// PAN ID (type 0x01): 2 bytes
	ds = append(ds, 0x01, 0x02, 0xAB, 0xCD)
	// Extended PAN ID (type 0x02): 8 bytes
	ds = append(ds, 0x02, 0x08, 0xDE, 0xAD, 0x00, 0xBE, 0xEF, 0x00, 0xCA, 0xFE)
	// Network Name (type 0x03): 7 bytes "TestNet"
	ds = append(ds, 0x03, 0x07, 'T', 'e', 's', 't', 'N', 'e', 't')
	// PSKc (type 0x04): 16 bytes
	ds = append(ds, 0x04, 0x10)
	ds = append(ds, make([]byte, 16)...)
	// Network Key (type 0x05): 16 bytes
	ds = append(ds, 0x05, 0x10)
	ds = append(ds, make([]byte, 16)...)
	// Mesh-Local Prefix (type 0x07): 8 bytes
	ds = append(ds, 0x07, 0x08, 0xFD, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
	// Security Policy (type 0x0C): 3 bytes
	ds = append(ds, 0x0C, 0x03, 0x00, 0xF8, 0x00)
	// Active Timestamp (type 0x0E): 8 bytes
	ds = append(ds, 0x0E, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00)
	return ds
}

// mockDiscoverer implements DeviceDiscoverer for testing.
type mockDiscoverer struct {
	addr string
	err  error
}

func (m *mockDiscoverer) DiscoverCommissionable(_ context.Context, _ uint16, _ DiscoveryCapabilities) (string, error) {
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

	// paseCallCount tracks how many times EstablishPASE has been called.
	paseCallCount int
	// paseErrAfterFirst, if non-nil, is returned for all EstablishPASE calls
	// AFTER the first successful one. Used to simulate reconnect failure.
	paseErrAfterFirst error
	// paseSucceedOnCall, if > 0, makes EstablishPASE return an error for all
	// calls until the Nth call (1-indexed), after which it succeeds.
	// Used to simulate "device not ready yet, succeeds on attempt N".
	paseSucceedOnCall int
}

func (m *mockSessionEstablisher) EstablishPASE(_ context.Context, _ string, _ uint32) (Session, error) {
	m.paseCallCount++

	// If paseSucceedOnCall is set, fail until we reach that call number.
	if m.paseSucceedOnCall > 0 && m.paseCallCount < m.paseSucceedOnCall {
		return nil, fmt.Errorf("device not ready (attempt %d of %d)", m.paseCallCount, m.paseSucceedOnCall)
	}

	// First call error.
	if m.paseErr != nil && m.paseCallCount == 1 {
		return nil, m.paseErr
	}

	// Subsequent calls error (e.g. reconnect always fails).
	if m.paseErrAfterFirst != nil && m.paseCallCount > 1 {
		return nil, m.paseErrAfterFirst
	}

	return m.paseSession, nil
}

func (m *mockSessionEstablisher) EstablishCASE(_ context.Context, _ string, _ uint64) (Session, error) {
	if m.caseErr != nil {
		return nil, m.caseErr
	}
	return m.caseSession, nil
}

// attrKey identifies a specific attribute for per-attribute mock responses.
type attrKey struct {
	endpoint  uint16
	cluster   uint32
	attribute uint32
}

// mockInteractionClient implements InteractionClient for testing.
type mockInteractionClient struct {
	invokeResp []byte
	invokeErr  error
	readResp   []byte
	readErr    error
	// readOverrides allows per-attribute responses. When a ReadAttribute call
	// matches a key here, the override is returned instead of readResp/readErr.
	readOverrides map[attrKey]struct {
		data []byte
		err  error
	}

	// invokeTimedCallCount tracks how many InvokeTimed calls have been made.
	invokeTimedCallCount int
	// invokeTimedErrOnCall, if non-nil, returns that error only on the
	// specified call number (1-indexed). All other calls use invokeErr/invokeResp.
	invokeTimedErrOnCall int
	invokeTimedErrValue  error
}

func (m *mockInteractionClient) Invoke(_ context.Context, _ Session, _ uint16, _, _ uint32, _ []byte) ([]byte, error) {
	return m.invokeResp, m.invokeErr
}

func (m *mockInteractionClient) InvokeTimed(_ context.Context, _ Session, _ uint16, _, _ uint32, _ []byte, _ uint16) ([]byte, error) {
	m.invokeTimedCallCount++
	if m.invokeTimedErrOnCall > 0 && m.invokeTimedCallCount == m.invokeTimedErrOnCall {
		return nil, m.invokeTimedErrValue
	}
	return m.invokeResp, m.invokeErr
}

func (m *mockInteractionClient) ReadAttribute(_ context.Context, _ Session, endpoint uint16, cluster, attribute uint32) ([]byte, error) {
	if m.readOverrides != nil {
		key := attrKey{endpoint, cluster, attribute}
		if ov, ok := m.readOverrides[key]; ok {
			return ov.data, ov.err
		}
	}
	return m.readResp, m.readErr
}

// encodeTLVUint32 produces a bare TLV unsigned integer element (anonymous tag)
// for use in mock attribute responses.
func encodeTLVUint32(v uint32) []byte {
	w := tlv.NewWriter()
	_ = w.PutUnsignedInt(tlv.AnonymousTag(), uint64(v))
	return w.Bytes()
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
		// Use zero-duration waits so BLE reconnect and CASE retry tests don't sleep.
		BLEReconnectInitialWait:   1 * time.Millisecond,
		BLEReconnectRetryInterval: 1 * time.Millisecond,
		BLEReconnectMaxAttempts:   3,
		CASEInitialWait:           1 * time.Millisecond,
		CASERetryInterval:         1 * time.Millisecond,
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

	threadCreds := NewThreadCredentials(testThreadDataset())
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

	// ReadCommissioningInfo should appear before ReadBasicInfo.
	hasReadCommInfo := false
	for _, s := range steps {
		if s == StepReadCommissioningInfo {
			hasReadCommInfo = true
			break
		}
	}
	if !hasReadCommInfo {
		t.Error("expected StepReadCommissioningInfo in progress steps")
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
		{StepReadCommissioningInfo, "ReadCommissioningInfo"},
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

func TestCommissioner_Commission_ThreadDeviceNoCredentials(t *testing.T) {
	c := newTestCommissioner()
	// Simulate BLE discovery so the device is NOT on-network.
	c.Discoverer = &mockDiscoverer{addr: "ble://mock"}

	// Override the NetworkCommissioning FeatureMap (cluster 0x0031, attr 0xFFFC)
	// to report Thread-only (bit 1 = 0x02).
	mc := c.Client.(*mockInteractionClient)
	mc.readOverrides = map[attrKey]struct {
		data []byte
		err  error
	}{
		{endpoint: 0, cluster: 0x0031, attribute: 0xFFFC}: {
			data: encodeTLVUint32(0x02), // Thread only
		},
	}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    1,
		// No Network credentials provided.
	}

	_, err := c.Commission(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for Thread device without credentials")
	}
	if !strings.Contains(err.Error(), "Thread network credentials") {
		t.Errorf("error should mention Thread credentials, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--thread-dataset") {
		t.Errorf("error should suggest --thread-dataset flag, got: %v", err)
	}
}

func TestCommissioner_Commission_WiFiThreadDeviceNoCredentials(t *testing.T) {
	c := newTestCommissioner()
	// Simulate BLE discovery so the device is NOT on-network.
	c.Discoverer = &mockDiscoverer{addr: "ble://mock"}

	// FeatureMap: WiFi (0x01) + Thread (0x02) = 0x03
	mc := c.Client.(*mockInteractionClient)
	mc.readOverrides = map[attrKey]struct {
		data []byte
		err  error
	}{
		{endpoint: 0, cluster: 0x0031, attribute: 0xFFFC}: {
			data: encodeTLVUint32(0x03), // WiFi + Thread
		},
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
		t.Fatal("expected error for WiFi+Thread device without credentials")
	}
	if !strings.Contains(err.Error(), "network credentials") {
		t.Errorf("error should mention network credentials, got: %v", err)
	}
}

func TestCommissioner_Commission_EthernetDeviceNoCredentials(t *testing.T) {
	c := newTestCommissioner()

	// FeatureMap: Ethernet (0x04) — no credentials needed.
	mc := c.Client.(*mockInteractionClient)
	mc.readOverrides = map[attrKey]struct {
		data []byte
		err  error
	}{
		{endpoint: 0, cluster: 0x0031, attribute: 0xFFFC}: {
			data: encodeTLVUint32(0x04), // Ethernet only
		},
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

	// Ethernet device should commission successfully without network creds.
	if _, err := c.Commission(context.Background(), params); err != nil {
		t.Fatalf("Commission should succeed for Ethernet device: %v", err)
	}
}

// TestCommissioner_Commission_OnNetworkSkipsFeatureMapCheck verifies that when
// a device is discovered via IP (on-network), the commissioning flow skips the
// NetworkCommissioning FeatureMap validation — even if the device incorrectly
// reports Thread-only capabilities.
func TestCommissioner_Commission_OnNetworkThreadDeviceNoCredentials(t *testing.T) {
	c := newTestCommissioner()
	// Default mock discoverer returns an IP address → auto-detected as on-network.

	// FeatureMap: Thread-only (0x02) — would normally require credentials,
	// but the device is on-network so this should be ignored.
	mc := c.Client.(*mockInteractionClient)
	mc.readOverrides = map[attrKey]struct {
		data []byte
		err  error
	}{
		{endpoint: 0, cluster: 0x0031, attribute: 0xFFFC}: {
			data: encodeTLVUint32(0x02), // Thread only
		},
	}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    1,
		// No Network credentials — device is on-network, so none needed.
	}

	// On-network device should succeed even with Thread-only FeatureMap.
	if _, err := c.Commission(context.Background(), params); err != nil {
		t.Fatalf("Commission should succeed for on-network device with Thread FeatureMap: %v", err)
	}
}

// TestCommissioner_Commission_OnNetworkIgnoresSuppliedCredentials verifies that
// when a device is on-network and the user supplies WiFi credentials, the
// credentials are ignored (with a warning) and commissioning succeeds without
// network provisioning.
func TestCommissioner_Commission_OnNetworkIgnoresSuppliedCredentials(t *testing.T) {
	c := newTestCommissioner()
	// Default mock discoverer returns an IP address → auto-detected as on-network.

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	wifiCreds := NewWiFiCredentials("TestNet", "TestPass")
	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    1,
		Network:   &wifiCreds,
	}

	// Should succeed — credentials are ignored for on-network devices.
	if _, err := c.Commission(context.Background(), params); err != nil {
		t.Fatalf("Commission should succeed (ignoring creds) for on-network device: %v", err)
	}
}

func TestCommissioner_Commission_ThreadDeviceWithCredentials(t *testing.T) {
	c := newTestCommissioner()
	// Simulate BLE discovery — credentials are provided for the Thread network.
	c.Discoverer = &mockDiscoverer{addr: "ble://mock"}

	// FeatureMap: Thread-only (0x02) — but credentials ARE provided.
	mc := c.Client.(*mockInteractionClient)
	mc.readOverrides = map[attrKey]struct {
		data []byte
		err  error
	}{
		{endpoint: 0, cluster: 0x0031, attribute: 0xFFFC}: {
			data: encodeTLVUint32(0x02), // Thread only
		},
	}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	threadCreds := NewThreadCredentials(testThreadDataset())
	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    1,
		Network:   &threadCreds,
	}

	// With Thread credentials provided, commissioning should proceed.
	if _, err := c.Commission(context.Background(), params); err != nil {
		t.Fatalf("Commission should succeed with Thread credentials: %v", err)
	}
}

// TestCommissioner_Commission_BLEDropDuringAddNOC_WithThread verifies that when
// the BLE connection drops during AddNOC and Thread credentials are pending,
// the commissioner reconnects BLE and delivers the Thread dataset.
func TestCommissioner_Commission_BLEDropDuringAddNOC_WithThread(t *testing.T) {
	c := newTestCommissioner()
	// Simulate BLE discovery — Thread credentials need to be delivered over BLE.
	c.Discoverer = &mockDiscoverer{addr: "ble://mock"}

	// Make InvokeTimed fail with ErrConnClosed on the 2nd call.
	// Call order: 1=AddTrustedRoot (TimedInvoke), 2=AddNOC (TimedInvoke).
	mc := c.Client.(*mockInteractionClient)
	mc.invokeTimedErrOnCall = 2
	mc.invokeTimedErrValue = fmt.Errorf("ble: %w", transport.ErrConnClosed)

	// The session establisher must handle two PASE calls:
	//   1st: initial PASE (succeeds)
	//   2nd: reconnect after AddNOC drop (succeeds)
	// Both return the same mockSession for simplicity.
	c.Sessions = &mockSessionEstablisher{
		paseSession: &mockSession{},
		caseSession: &mockSession{},
	}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	threadCreds := NewThreadCredentials(testThreadDataset())
	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    4,
		Network:   &threadCreds,
	}

	_, err := c.Commission(context.Background(), params)
	if err != nil {
		t.Fatalf("Commission should succeed after BLE reconnect: %v", err)
	}

	// EstablishPASE must have been called twice: once initially and once for
	// the reconnect after AddNOC dropped the BLE link.
	se := c.Sessions.(*mockSessionEstablisher)
	if se.paseCallCount != 2 {
		t.Errorf("EstablishPASE call count: got %d, want 2 (initial + reconnect)", se.paseCallCount)
	}
}

// TestCommissioner_Commission_BLEDropDuringAddNOC_ReconnectFails verifies that
// when BLE drops during AddNOC and all reconnect attempts fail, Commission
// returns an error explaining that the Thread dataset could not be delivered.
func TestCommissioner_Commission_BLEDropDuringAddNOC_ReconnectFails(t *testing.T) {
	c := newTestCommissioner()
	// Simulate BLE discovery — Thread credentials need to be delivered over BLE.
	c.Discoverer = &mockDiscoverer{addr: "ble://mock"}

	// Make InvokeTimed fail with ErrConnClosed on the 2nd call (AddNOC).
	mc := c.Client.(*mockInteractionClient)
	mc.invokeTimedErrOnCall = 2
	mc.invokeTimedErrValue = fmt.Errorf("ble: %w", transport.ErrConnClosed)

	// All reconnect PASE attempts will fail (simulating device not coming
	// back online, e.g. crashed permanently or failsafe timer expired).
	c.Sessions = &mockSessionEstablisher{
		paseSession:       &mockSession{},
		caseSession:       &mockSession{},
		paseErrAfterFirst: fmt.Errorf("device not advertising"),
	}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	threadCreds := NewThreadCredentials(testThreadDataset())
	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    5,
		Network:   &threadCreds,
	}

	_, err := c.Commission(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when BLE reconnect fails after AddNOC")
	}
	if !strings.Contains(err.Error(), "reconnect") {
		t.Errorf("error should mention reconnect, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Thread") {
		t.Errorf("error should mention Thread network type, got: %v", err)
	}
}

// TestCommissioner_Commission_BLEDropDuringAddNOC_WithWiFi verifies the same
// reconnect logic works for WiFi devices (not just Thread).
func TestCommissioner_Commission_BLEDropDuringAddNOC_WithWiFi(t *testing.T) {
	c := newTestCommissioner()
	// Simulate BLE discovery — WiFi credentials need to be delivered over BLE.
	c.Discoverer = &mockDiscoverer{addr: "ble://mock"}

	// Make InvokeTimed fail with ErrConnClosed on the 2nd call (AddNOC).
	mc := c.Client.(*mockInteractionClient)
	mc.invokeTimedErrOnCall = 2
	mc.invokeTimedErrValue = fmt.Errorf("ble: %w", transport.ErrConnClosed)

	c.Sessions = &mockSessionEstablisher{
		paseSession: &mockSession{},
		caseSession: &mockSession{},
	}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	wifiCreds := NewWiFiCredentials("TestNet", "TestPass")
	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    6,
		Network:   &wifiCreds,
	}

	_, err := c.Commission(context.Background(), params)
	if err != nil {
		t.Fatalf("Commission should succeed after BLE reconnect (WiFi): %v", err)
	}

	se := c.Sessions.(*mockSessionEstablisher)
	if se.paseCallCount != 2 {
		t.Errorf("EstablishPASE call count: got %d, want 2 (initial + reconnect)", se.paseCallCount)
	}
}

// TestCommissioner_Commission_BLEDropDuringAddNOC_NoNetworkCreds verifies that
// when BLE drops during AddNOC but no network credentials are needed (e.g.
// Ethernet device), the commissioner does NOT attempt to reconnect BLE and
// proceeds directly to CASE — the existing optimistic path.
func TestCommissioner_Commission_BLEDropDuringAddNOC_NoNetworkCreds(t *testing.T) {
	c := newTestCommissioner()

	// Make InvokeTimed fail with ErrConnClosed on the 2nd call (AddNOC).
	mc := c.Client.(*mockInteractionClient)
	mc.invokeTimedErrOnCall = 2
	mc.invokeTimedErrValue = fmt.Errorf("ble: %w", transport.ErrConnClosed)

	c.Sessions = &mockSessionEstablisher{
		paseSession: &mockSession{},
		caseSession: &mockSession{},
	}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	// No network credentials — device is on Ethernet or already has
	// Thread/WiFi credentials from a previous commissioning.
	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    7,
		Network:   nil,
	}

	_, err := c.Commission(context.Background(), params)
	if err != nil {
		t.Fatalf("Commission should succeed (optimistic path) when no network creds needed: %v", err)
	}

	// EstablishPASE should only have been called once — no reconnect needed.
	se := c.Sessions.(*mockSessionEstablisher)
	if se.paseCallCount != 1 {
		t.Errorf("EstablishPASE call count: got %d, want 1 (no reconnect when no network creds)", se.paseCallCount)
	}
}

// TestCommissioner_Commission_ReadsSupportsConcurrentConnection verifies that
// the commissioner reads SupportsConcurrentConnection (GeneralCommissioning
// cluster 0x0030, attribute 0x0004) after arming the failsafe. When the
// attribute is false, BLE drops during AddNOC are expected (not a surprise).
func TestCommissioner_Commission_ReadsSupportsConcurrentConnection(t *testing.T) {
	c := newTestCommissioner()

	// Encode a TLV boolean "true" for SupportsConcurrentConnection.
	w := tlv.NewWriter()
	_ = w.PutBool(tlv.AnonymousTag(), true)
	sccTrue := w.Bytes()

	mc := c.Client.(*mockInteractionClient)
	if mc.readOverrides == nil {
		mc.readOverrides = make(map[attrKey]struct {
			data []byte
			err  error
		})
	}
	// GeneralCommissioning (0x0030), SupportsConcurrentConnection (0x0004)
	mc.readOverrides[attrKey{0, 0x0030, 0x0004}] = struct {
		data []byte
		err  error
	}{data: sccTrue, err: nil}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    10,
	}

	_, err := c.Commission(context.Background(), params)
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
}

// TestCommissioner_Commission_NonConcurrentDevice verifies that when
// SupportsConcurrentConnection is false and BLE drops during AddNOC,
// the commissioner logs it as expected behaviour (not a surprise).
func TestCommissioner_Commission_NonConcurrentDevice(t *testing.T) {
	c := newTestCommissioner()

	// Encode a TLV boolean "false" for SupportsConcurrentConnection.
	w := tlv.NewWriter()
	_ = w.PutBool(tlv.AnonymousTag(), false)
	sccFalse := w.Bytes()

	mc := c.Client.(*mockInteractionClient)
	if mc.readOverrides == nil {
		mc.readOverrides = make(map[attrKey]struct {
			data []byte
			err  error
		})
	}
	mc.readOverrides[attrKey{0, 0x0030, 0x0004}] = struct {
		data []byte
		err  error
	}{data: sccFalse, err: nil}

	// Make InvokeTimed fail with ErrConnClosed on AddNOC (2nd timed call).
	mc.invokeTimedErrOnCall = 2
	mc.invokeTimedErrValue = fmt.Errorf("ble: %w", transport.ErrConnClosed)

	// No network creds → Ethernet device, so we go straight to CASE.
	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    11,
		Network:   nil,
	}

	_, err := c.Commission(context.Background(), params)
	if err != nil {
		t.Fatalf("Commission should succeed for non-concurrent device with BLE drop: %v", err)
	}

	// Only one PASE call — no reconnect needed since no network creds.
	se := c.Sessions.(*mockSessionEstablisher)
	if se.paseCallCount != 1 {
		t.Errorf("EstablishPASE call count: got %d, want 1", se.paseCallCount)
	}
}

// TestCommissioner_Commission_NonConcurrentDeviceWithThread verifies that
// a non-concurrent device with Thread credentials triggers BLE reconnect
// after AddNOC drops BLE, and commissioning succeeds.
func TestCommissioner_Commission_NonConcurrentDeviceWithThread(t *testing.T) {
	c := newTestCommissioner()
	// Simulate BLE discovery — Thread credentials need to be delivered over BLE.
	c.Discoverer = &mockDiscoverer{addr: "ble://mock"}

	// SupportsConcurrentConnection = false
	w := tlv.NewWriter()
	_ = w.PutBool(tlv.AnonymousTag(), false)
	sccFalse := w.Bytes()

	mc := c.Client.(*mockInteractionClient)
	if mc.readOverrides == nil {
		mc.readOverrides = make(map[attrKey]struct {
			data []byte
			err  error
		})
	}
	mc.readOverrides[attrKey{0, 0x0030, 0x0004}] = struct {
		data []byte
		err  error
	}{data: sccFalse, err: nil}

	// BLE drops on AddNOC.
	mc.invokeTimedErrOnCall = 2
	mc.invokeTimedErrValue = fmt.Errorf("ble: %w", transport.ErrConnClosed)

	c.Sessions = &mockSessionEstablisher{
		paseSession: &mockSession{},
		caseSession: &mockSession{},
	}

	payload := SetupPayload{
		Discriminator: 3840,
		Passcode:      20202021,
	}
	qr, _ := payload.QRCode()

	threadCreds := NewThreadCredentials(testThreadDataset())
	params := CommissioningParams{
		SetupCode: qr,
		NodeID:    12,
		Network:   &threadCreds,
	}

	_, err := c.Commission(context.Background(), params)
	if err != nil {
		t.Fatalf("Commission should succeed for non-concurrent Thread device: %v", err)
	}

	// Two PASE calls: initial + reconnect after AddNOC BLE drop.
	se := c.Sessions.(*mockSessionEstablisher)
	if se.paseCallCount != 2 {
		t.Errorf("EstablishPASE call count: got %d, want 2 (initial + reconnect)", se.paseCallCount)
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
