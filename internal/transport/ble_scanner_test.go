// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package transport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Mock adapter ─────────────────────────────────────────────────────────────

// mockBLEAdapter is a pure-Go test double for bleAdapter. It replays a
// pre-configured sequence of BLEScanAdvertisement events and can simulate
// connection establishment without any BLE hardware or CGo.
type mockBLEAdapter struct {
	mu sync.Mutex

	// advertisements is the ordered sequence of events that Scan will emit.
	// Each advertisement is delivered before Scan blocks for delay[i].
	advertisements []BLEScanAdvertisement

	// delay between delivering each advertisement (0 = immediate).
	delay time.Duration

	// scanErr is the error Scan returns after all advertisements are delivered
	// (nil = return ctx.Err() as normal).
	scanErr error

	// connectFn is called by Connect; if nil, Connect returns a mockBLEDevice.
	connectFn func(ctx context.Context, addr BLEAddress) (bleDevice, error)

	// enableErr is the error returned by Enable.
	enableErr error

	// scanCalled tracks how many times Scan was called.
	scanCalled int

	// stopScanCalled tracks how many times StopScan was called.
	stopScanCalled int

	// scanRunning is true while a Scan invocation is in progress.
	scanRunning bool
}

func (m *mockBLEAdapter) Enable() error {
	return m.enableErr
}

func (m *mockBLEAdapter) Scan(ctx context.Context, cb func(BLEScanAdvertisement)) error {
	m.mu.Lock()
	m.scanCalled++
	advs := m.advertisements
	delay := m.delay
	scanErr := m.scanErr
	m.scanRunning = true
	m.mu.Unlock()

	for _, adv := range advs {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			m.scanRunning = false
			m.mu.Unlock()
			return ctx.Err()
		default:
		}

		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				m.mu.Lock()
				m.scanRunning = false
				m.mu.Unlock()
				return ctx.Err()
			}
		}

		cb(adv)
	}

	m.mu.Lock()
	m.scanRunning = false
	m.mu.Unlock()

	if scanErr != nil {
		return scanErr
	}

	// Block until context is cancelled (mirrors real adapter behaviour).
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockBLEAdapter) StopScan() error {
	m.mu.Lock()
	m.stopScanCalled++
	m.mu.Unlock()
	return nil
}

func (m *mockBLEAdapter) Connect(ctx context.Context, addr BLEAddress) (bleDevice, error) {
	if m.connectFn != nil {
		return m.connectFn(ctx, addr)
	}
	return &mockBLEDevice{}, nil
}

// ─── Mock device / service / characteristic ───────────────────────────────────

type mockBLEDevice struct {
	services       []bleService
	discoverErr    error
	disconnectErr  error
	disconnectCalled bool
}

func (d *mockBLEDevice) DiscoverServices(uuids []BLEUUID) ([]bleService, error) {
	if d.discoverErr != nil {
		return nil, d.discoverErr
	}
	if len(uuids) == 0 {
		return d.services, nil
	}
	// Filter by requested UUIDs.
	var filtered []bleService
	for _, svc := range d.services {
		for _, u := range uuids {
			if svc.UUID() == u {
				filtered = append(filtered, svc)
				break
			}
		}
	}
	return filtered, nil
}

func (d *mockBLEDevice) Disconnect() error {
	d.disconnectCalled = true
	return d.disconnectErr
}

type mockBLEService struct {
	uuid            BLEUUID
	characteristics []bleCharacteristic
	discoverErr     error
}

func (s *mockBLEService) UUID() BLEUUID { return s.uuid }

func (s *mockBLEService) DiscoverCharacteristics(uuids []BLEUUID) ([]bleCharacteristic, error) {
	if s.discoverErr != nil {
		return nil, s.discoverErr
	}
	if len(uuids) == 0 {
		return s.characteristics, nil
	}
	var filtered []bleCharacteristic
	for _, c := range s.characteristics {
		for _, u := range uuids {
			if c.UUID() == u {
				filtered = append(filtered, c)
				break
			}
		}
	}
	return filtered, nil
}

type mockBLECharacteristic struct {
	uuid              BLEUUID
	writeData         [][]byte
	writeMu           sync.Mutex
	writeErr          error
	notifCb           func([]byte)
	enableNotifErr    error
	waitCh            chan []byte // delivers data for WaitForValue
}

func (c *mockBLECharacteristic) UUID() BLEUUID { return c.uuid }

func (c *mockBLECharacteristic) Write(data []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	c.writeMu.Lock()
	c.writeData = append(c.writeData, cp)
	c.writeMu.Unlock()
	return len(data), nil
}

func (c *mockBLECharacteristic) EnableNotifications(cb func([]byte)) error {
	if c.enableNotifErr != nil {
		return c.enableNotifErr
	}
	c.notifCb = cb
	return nil
}

func (c *mockBLECharacteristic) WaitForNotifying(_ context.Context) error {
	return nil
}

func (c *mockBLECharacteristic) WaitForValue(ctx context.Context) ([]byte, error) {
	if c.waitCh == nil {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	select {
	case data := <-c.waitCh:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ClearCachedValue discards any pending value from waitCh without blocking.
func (c *mockBLECharacteristic) ClearCachedValue() {
	if c.waitCh == nil {
		return
	}
	select {
	case <-c.waitCh:
	default:
	}
}

// ReadAndClearCachedValue atomically reads and removes a pending value from
// waitCh. Returns nil if no value is currently available.
func (c *mockBLECharacteristic) ReadAndClearCachedValue() []byte {
	if c.waitCh == nil {
		return nil
	}
	select {
	case data := <-c.waitCh:
		return data
	default:
		return nil
	}
}

// writtenData returns a copy of all byte slices written to this characteristic.
func (c *mockBLECharacteristic) writtenData() [][]byte {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	out := make([][]byte, len(c.writeData))
	copy(out, c.writeData)
	return out
}

// ─── Advertisement builder helpers ───────────────────────────────────────────

// makeMatterServiceData constructs a valid Matter commissioning service data
// payload per spec §5.4.2.5.6.
func makeMatterServiceData(discriminator uint16, vendorID, productID uint16) []byte {
	data := make([]byte, 8)
	data[0] = matterAdvOpCode // OpCode = 0x00
	// Byte 1–2: discriminator (bits [11:0]) + version nibble (bits [15:12] = 0)
	discAndVer := discriminator & 0x0FFF
	data[1] = byte(discAndVer)
	data[2] = byte(discAndVer >> 8)
	data[3] = byte(vendorID)
	data[4] = byte(vendorID >> 8)
	data[5] = byte(productID)
	data[6] = byte(productID >> 8)
	data[7] = 0x00 // no additional data
	return data
}

// makeMatterAdv builds a BLEScanAdvertisement that looks like a Matter device.
func makeMatterAdv(addr BLEAddress, discriminator, vendorID, productID uint16, rssi int16, name string) BLEScanAdvertisement {
	return BLEScanAdvertisement{
		Address:   addr,
		RSSI:      rssi,
		LocalName: name,
		ServiceData: map[BLEUUID][]byte{
			MatterServiceUUID: makeMatterServiceData(discriminator, vendorID, productID),
		},
	}
}

// ─── parseMatterAdvertisement tests ──────────────────────────────────────────

func TestParseMatterAdvertisement_Valid(t *testing.T) {
	tests := []struct {
		name          string
		discriminator uint16
		vendorID      uint16
		productID     uint16
		rssi          int16
		localName     string
	}{
		{
			name:          "typical device",
			discriminator: 0xABC,
			vendorID:      0xFFF1,
			productID:     0x8000,
			rssi:          -60,
			localName:     "MatterLight",
		},
		{
			name:          "zero values",
			discriminator: 0,
			vendorID:      0,
			productID:     0,
			rssi:          0,
			localName:     "",
		},
		{
			name:          "max discriminator",
			discriminator: 0x0FFF,
			vendorID:      0xFFFF,
			productID:     0xFFFF,
			rssi:          -100,
			localName:     "MaxDevice",
		},
		{
			name:          "discriminator masked to 12 bits",
			discriminator: 0x0ABC,
			vendorID:      0x1234,
			productID:     0x5678,
			rssi:          -75,
			localName:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adv := makeMatterAdv("AA:BB:CC:DD:EE:FF", tt.discriminator, tt.vendorID, tt.productID, tt.rssi, tt.localName)
			sr, ok := parseMatterAdvertisement(adv)
			require.True(t, ok, "expected advertisement to be parsed as Matter")
			assert.Equal(t, BLEAddress("AA:BB:CC:DD:EE:FF"), sr.Address)
			assert.Equal(t, tt.discriminator, sr.Discriminator)
			assert.Equal(t, tt.vendorID, sr.VendorID)
			assert.Equal(t, tt.productID, sr.ProductID)
			assert.Equal(t, tt.rssi, sr.RSSI)
			assert.Equal(t, tt.localName, sr.Name)
		})
	}
}

func TestParseMatterAdvertisement_DiscriminatorMasking(t *testing.T) {
	// The discriminator is stored in bits [11:0]; upper nibble is the version.
	// If the version nibble is non-zero the discriminator should still be masked.
	data := makeMatterServiceData(0x0ABC, 0x1234, 0x5678)
	// Set a non-zero version nibble in the upper bits.
	data[2] |= 0xF0 // bits [15:12] = 0xF (version = 15)
	adv := BLEScanAdvertisement{
		Address:     "AA:BB:CC:DD:EE:FF",
		ServiceData: map[BLEUUID][]byte{MatterServiceUUID: data},
	}
	sr, ok := parseMatterAdvertisement(adv)
	require.True(t, ok)
	// Discriminator must be masked to 12 bits.
	assert.Equal(t, uint16(0x0ABC), sr.Discriminator)
}

func TestParseMatterAdvertisement_NoMatterServiceData(t *testing.T) {
	adv := BLEScanAdvertisement{
		Address:     "AA:BB:CC:DD:EE:FF",
		ServiceData: map[BLEUUID][]byte{},
	}
	_, ok := parseMatterAdvertisement(adv)
	assert.False(t, ok, "should reject advertisement with no Matter service data")
}

func TestParseMatterAdvertisement_NilServiceData(t *testing.T) {
	adv := BLEScanAdvertisement{
		Address:     "AA:BB:CC:DD:EE:FF",
		ServiceData: nil,
	}
	_, ok := parseMatterAdvertisement(adv)
	assert.False(t, ok)
}

func TestParseMatterAdvertisement_TooShort(t *testing.T) {
	// Payloads of 1–7 bytes are malformed and must be rejected.
	malformed := []struct {
		name string
		data []byte
	}{
		{"1 byte", []byte{0x00}},
		{"7 bytes (one short)", []byte{0x00, 0xBC, 0x0A, 0xF1, 0xFF, 0x00, 0x80}},
	}
	for _, tt := range malformed {
		t.Run(tt.name, func(t *testing.T) {
			adv := BLEScanAdvertisement{
				Address: "AA:BB:CC:DD:EE:FF",
				ServiceData: map[BLEUUID][]byte{
					MatterServiceUUID: tt.data,
				},
			}
			_, ok := parseMatterAdvertisement(adv)
			assert.False(t, ok, "should reject payload of length %d", len(tt.data))
		})
	}
}

// TestParseMatterAdvertisement_NilPayload verifies that a nil (zero-length)
// service data entry — synthesised by the adapter when CoreBluetooth exposes
// the Matter UUID in ServiceUUIDs but omits the service data payload — is
// accepted and returns a partial ScanResult so the device is still visible.
func TestParseMatterAdvertisement_NilPayload(t *testing.T) {
	adv := BLEScanAdvertisement{
		Address:   "AA:BB:CC:DD:EE:FF",
		RSSI:      -60,
		LocalName: "MatterDevice",
		ServiceData: map[BLEUUID][]byte{
			MatterServiceUUID: nil, // synthesised when service data absent
		},
	}
	sr, ok := parseMatterAdvertisement(adv)
	assert.True(t, ok, "nil payload should return ok=true (partial result)")
	assert.Equal(t, BLEAddress("AA:BB:CC:DD:EE:FF"), sr.Address)
	assert.Equal(t, int16(-60), sr.RSSI)
	assert.Equal(t, "MatterDevice", sr.Name)
	// Discriminator and VID/PID are zero because the payload was absent.
	assert.Equal(t, uint16(0), sr.Discriminator)
	assert.Equal(t, uint16(0), sr.VendorID)
	assert.Equal(t, uint16(0), sr.ProductID)
}

func TestParseMatterAdvertisement_WrongOpCode(t *testing.T) {
	data := makeMatterServiceData(0x123, 0xFFF1, 0x8000)
	data[0] = 0xFF // invalid OpCode
	adv := BLEScanAdvertisement{
		Address:     "AA:BB:CC:DD:EE:FF",
		ServiceData: map[BLEUUID][]byte{MatterServiceUUID: data},
	}
	_, ok := parseMatterAdvertisement(adv)
	assert.False(t, ok, "should reject advertisement with wrong OpCode")
}

func TestParseMatterAdvertisement_ExtraBytes(t *testing.T) {
	// Payload longer than 8 bytes should still be accepted (future extensibility).
	data := makeMatterServiceData(0x456, 0x1234, 0x5678)
	data = append(data, 0xDE, 0xAD, 0xBE, 0xEF) // extra bytes
	adv := BLEScanAdvertisement{
		Address:     "AA:BB:CC:DD:EE:FF",
		ServiceData: map[BLEUUID][]byte{MatterServiceUUID: data},
	}
	sr, ok := parseMatterAdvertisement(adv)
	require.True(t, ok, "should accept payload longer than minimum")
	assert.Equal(t, uint16(0x456), sr.Discriminator)
}

// ─── BLEScanner.Scan tests ────────────────────────────────────────────────────

func TestBLEScanner_Scan_EmptyResults(t *testing.T) {
	adapter := &mockBLEAdapter{}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results, err := scanner.Scan(ctx)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestBLEScanner_Scan_SingleMatterDevice(t *testing.T) {
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			makeMatterAdv("AA:BB:CC:DD:EE:FF", 0x123, 0xFFF1, 0x8000, -60, "Light"),
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	results, err := scanner.Scan(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	assert.Equal(t, BLEAddress("AA:BB:CC:DD:EE:FF"), r.Address)
	assert.Equal(t, uint16(0x123), r.Discriminator)
	assert.Equal(t, uint16(0xFFF1), r.VendorID)
	assert.Equal(t, uint16(0x8000), r.ProductID)
	assert.Equal(t, int16(-60), r.RSSI)
	assert.Equal(t, "Light", r.Name)
}

func TestBLEScanner_Scan_IgnoresNonMatterDevices(t *testing.T) {
	nonMatter := BLEScanAdvertisement{
		Address:     "11:22:33:44:55:66",
		RSSI:        -50,
		LocalName:   "SomeFitnessBand",
		ServiceData: map[BLEUUID][]byte{
			// Different service UUID, not Matter.
			"00001800-0000-1000-8000-00805f9b34fb": {0x01, 0x02},
		},
	}
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			nonMatter,
			makeMatterAdv("AA:BB:CC:DD:EE:FF", 0x456, 0xFFF1, 0x8001, -70, ""),
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	results, err := scanner.Scan(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1, "only the Matter device should appear")
	assert.Equal(t, BLEAddress("AA:BB:CC:DD:EE:FF"), results[0].Address)
}

func TestBLEScanner_Scan_MultipleDevices(t *testing.T) {
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			makeMatterAdv("AA:BB:CC:DD:EE:01", 0x001, 0xFFF1, 0x8000, -80, "Device1"),
			makeMatterAdv("AA:BB:CC:DD:EE:02", 0x002, 0xFFF1, 0x8001, -55, "Device2"),
			makeMatterAdv("AA:BB:CC:DD:EE:03", 0x003, 0xFFF2, 0x8002, -70, "Device3"),
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	results, err := scanner.Scan(ctx)
	require.NoError(t, err)
	require.Len(t, results, 3)
}

func TestBLEScanner_Scan_SortedByRSSIDescending(t *testing.T) {
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			makeMatterAdv("AA:BB:CC:DD:EE:01", 0x001, 0xFFF1, 0x8000, -90, "Weak"),
			makeMatterAdv("AA:BB:CC:DD:EE:02", 0x002, 0xFFF1, 0x8001, -40, "Strong"),
			makeMatterAdv("AA:BB:CC:DD:EE:03", 0x003, 0xFFF2, 0x8002, -65, "Medium"),
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	results, err := scanner.Scan(ctx)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// First result must have the highest (least negative) RSSI.
	assert.Equal(t, int16(-40), results[0].RSSI, "strongest device should be first")
	assert.Equal(t, int16(-65), results[1].RSSI)
	assert.Equal(t, int16(-90), results[2].RSSI, "weakest device should be last")
}

func TestBLEScanner_Scan_DeduplicatesByAddress(t *testing.T) {
	// Same address appears twice; we should only get one result with the
	// higher RSSI.
	addr := BLEAddress("AA:BB:CC:DD:EE:FF")
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			makeMatterAdv(addr, 0x123, 0xFFF1, 0x8000, -80, "Light"),
			makeMatterAdv(addr, 0x123, 0xFFF1, 0x8000, -55, "Light"), // stronger sighting
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	results, err := scanner.Scan(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1, "duplicate address should be deduplicated")
	assert.Equal(t, int16(-55), results[0].RSSI, "should keep the strongest RSSI observation")
}

func TestBLEScanner_Scan_DeduplicateKeepsWeakerRSSIUnchanged(t *testing.T) {
	// First sighting is stronger; second weaker — RSSI should stay at the
	// original stronger value.
	addr := BLEAddress("AA:BB:CC:DD:EE:FF")
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			makeMatterAdv(addr, 0x123, 0xFFF1, 0x8000, -45, "Light"), // stronger
			makeMatterAdv(addr, 0x123, 0xFFF1, 0x8000, -90, "Light"), // weaker
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	results, err := scanner.Scan(ctx)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, int16(-45), results[0].RSSI, "RSSI should not decrease on duplicate")
}

func TestBLEScanner_Scan_AdapterError(t *testing.T) {
	wantErr := errors.New("adapter failed")
	adapter := &mockBLEAdapter{
		scanErr: wantErr,
	}
	scanner := NewBLEScanner(adapter)

	ctx := context.Background()
	_, err := scanner.Scan(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestBLEScanner_Scan_ContextCancelIsNotError(t *testing.T) {
	// A clean context cancellation should return nil error and whatever
	// results were found before cancellation.
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			makeMatterAdv("AA:BB:CC:DD:EE:FF", 0x123, 0xFFF1, 0x8000, -60, "Light"),
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	results, err := scanner.Scan(ctx)
	require.NoError(t, err, "context cancel should not be treated as error")
	_ = results
}

func TestBLEScanner_Scan_TimeoutIsNotError(t *testing.T) {
	adapter := &mockBLEAdapter{}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := scanner.Scan(ctx)
	require.NoError(t, err, "timeout should not be returned as an error")
}

// ─── BLEScanner.FindByDiscriminator tests ────────────────────────────────────

func TestBLEScanner_FindByDiscriminator_Found(t *testing.T) {
	target := BLEAddress("AA:BB:CC:DD:EE:FF")
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			makeMatterAdv("11:22:33:44:55:66", 0x100, 0xFFF1, 0x8000, -80, "Other"),
			makeMatterAdv(target, 0x456, 0xFFF1, 0x8001, -60, "Target"),
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := scanner.FindByDiscriminator(ctx, 0x456)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, target, result.Address)
	assert.Equal(t, uint16(0x456), result.Discriminator)
}

func TestBLEScanner_FindByDiscriminator_NotFound_Timeout(t *testing.T) {
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			makeMatterAdv("AA:BB:CC:DD:EE:FF", 0x100, 0xFFF1, 0x8000, -60, "WrongDevice"),
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := scanner.FindByDiscriminator(ctx, 0x999)
	require.Error(t, err, "should return error when target not found")
	assert.Nil(t, result)
	// discriminator 0x999 = 2457 decimal — the error message uses decimal formatting
	assert.Contains(t, err.Error(), "2457")
}

func TestBLEScanner_FindByDiscriminator_StopsAfterFound(t *testing.T) {
	// Verify that after finding the target the scan stops — the context passed
	// to Scan is cancelled — so subsequent advertisements are not processed.
	// We check that the adapter's scan is called exactly once.
	target := BLEAddress("AA:BB:CC:DD:EE:FF")
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			makeMatterAdv(target, 0x456, 0xFFF1, 0x8001, -60, "Target"),
			// These come after the target — should not be processed if scan stops.
			makeMatterAdv("11:22:33:44:55:66", 0x789, 0xFFF2, 0x8002, -55, "Extra"),
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := scanner.FindByDiscriminator(ctx, 0x456)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, target, result.Address)

	adapter.mu.Lock()
	called := adapter.scanCalled
	adapter.mu.Unlock()
	assert.Equal(t, 1, called, "Scan should be called exactly once")
}

func TestBLEScanner_FindByDiscriminator_ShortDiscriminator(t *testing.T) {
	// Manual pairing codes encode only the upper 4 bits of the 12-bit
	// discriminator as shortDisc<<8 (lower 8 bits zero). The scanner
	// must match on the upper 4 bits only when this pattern is detected.
	target := BLEAddress("AA:BB:CC:DD:EE:FF")
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			// Device with full discriminator 0x0EB1 — short disc = 0x0E.
			makeMatterAdv(target, 0x0EB1, 0x130A, 0x0050, -60, "EveEnergy"),
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Manual pairing code encodes short discriminator 0x0E as 0x0E00.
	result, err := scanner.FindByDiscriminator(ctx, 0x0E00)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, target, result.Address)
	assert.Equal(t, uint16(0x0EB1), result.Discriminator)
}

func TestBLEScanner_FindByDiscriminator_ShortDiscriminatorNoMatch(t *testing.T) {
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			// Device with full discriminator 0x0EB1 — short disc = 0x0E.
			makeMatterAdv("AA:BB:CC:DD:EE:FF", 0x0EB1, 0x130A, 0x0050, -60, "EveEnergy"),
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Short discriminator 0x0F (= 0x0F00) should NOT match 0x0EB1 (short = 0x0E).
	result, err := scanner.FindByDiscriminator(ctx, 0x0F00)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestBLEScanner_FindByDiscriminator_FirstDeviceAmongMany(t *testing.T) {
	// Even when multiple devices are in range, we should return the one with
	// the matching discriminator.
	adapter := &mockBLEAdapter{
		advertisements: []BLEScanAdvertisement{
			makeMatterAdv("AA:00:00:00:00:01", 0x001, 0xFFF1, 0x8000, -80, "D1"),
			makeMatterAdv("AA:00:00:00:00:02", 0x002, 0xFFF1, 0x8001, -70, "D2"),
			makeMatterAdv("AA:00:00:00:00:03", 0x003, 0xFFF1, 0x8002, -60, "D3"),
		},
	}
	scanner := NewBLEScanner(adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := scanner.FindByDiscriminator(ctx, 0x002)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, BLEAddress("AA:00:00:00:00:02"), result.Address)
}

// ─── NewBLEScanner tests ──────────────────────────────────────────────────────

func TestNewBLEScanner_PanicsOnNilAdapter(t *testing.T) {
	assert.Panics(t, func() {
		NewBLEScanner(nil)
	})
}

func TestNewBLEScanner_ReturnsScannerWithAdapter(t *testing.T) {
	adapter := &mockBLEAdapter{}
	scanner := NewBLEScanner(adapter)
	require.NotNil(t, scanner)
	assert.Equal(t, adapter, scanner.adapter)
}

// ─── BLEAddress / BLEUUID type tests ─────────────────────────────────────────

func TestBLEAddress_String(t *testing.T) {
	addr := BLEAddress("AA:BB:CC:DD:EE:FF")
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", addr.String())
}

func TestBLEAddress_Network(t *testing.T) {
	addr := BLEAddress("AA:BB:CC:DD:EE:FF")
	assert.Equal(t, "ble", addr.Network())
}

func TestBLEUUID_String(t *testing.T) {
	u := MatterServiceUUID
	assert.Equal(t, "0000fff6-0000-1000-8000-00805f9b34fb", u.String())
}

// ─── Matter service UUID constant tests ──────────────────────────────────────

func TestMatterServiceUUIDs_AreCorrect(t *testing.T) {
	// Values from Matter spec §5.5 / BLE commissioning UUIDs.
	assert.Equal(t, BLEUUID("0000fff6-0000-1000-8000-00805f9b34fb"), MatterServiceUUID)
	assert.Equal(t, BLEUUID("18ee2ef5-263d-4559-959f-4f9c429f9d11"), MatterC1UUID)
	assert.Equal(t, BLEUUID("18ee2ef5-263d-4559-959f-4f9c429f9d12"), MatterC2UUID)
	assert.Equal(t, BLEUUID("64630238-8772-45f2-b87d-748a83218f04"), MatterC3UUID)
}

// ─── Mock adapter interface compliance ───────────────────────────────────────

// Compile-time checks: ensure mock types satisfy the interfaces.
var _ bleAdapter        = (*mockBLEAdapter)(nil)
var _ bleDevice         = (*mockBLEDevice)(nil)
var _ bleService        = (*mockBLEService)(nil)
var _ bleCharacteristic = (*mockBLECharacteristic)(nil)
