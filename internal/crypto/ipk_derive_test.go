package crypto

import (
	"encoding/hex"
	"testing"
)

func TestOperationalIPKDerivation(t *testing.T) {
	// Values from actual commissioning attempt:
	// Raw IPK sent via AddNOC:
	rawIPK, _ := hex.DecodeString("e074cefd704454d4cef01f0fda40eb48")

	// Root public key from the RCAC (uncompressed, 65 bytes with 0x04 prefix):
	rootPubKey, _ := hex.DecodeString("040737d9ed6aa2e235085125d57a5deda744021e2e4d1d11a36a0602528f48d4ab85463e82944e07574ce57f4c2245503afc26ef66fb6232997d7d6c7d4b266eec")

	// Fabric ID = 1
	var fabricID uint64 = 1

	// Device's operational key (from matter.js debug log):
	expectedOpKey := "8ac33959ab6c8fa7fef7ec29adcd0669"

	// Step 1: Compute compressed fabric ID.
	compressedID, err := CompressedFabricID(rootPubKey, fabricID)
	if err != nil {
		t.Fatalf("CompressedFabricID: %v", err)
	}
	t.Logf("Compressed Fabric ID: %s", hex.EncodeToString(compressedID))

	// Step 2: Derive operational IPK.
	opIPK, err := DeriveGroupOperationalKey(rawIPK, compressedID)
	if err != nil {
		t.Fatalf("DeriveGroupOperationalKey: %v", err)
	}
	opIPKHex := hex.EncodeToString(opIPK)
	t.Logf("Operational IPK: %s", opIPKHex)
	t.Logf("Expected:        %s", expectedOpKey)

	if opIPKHex != expectedOpKey {
		t.Fatalf("operational IPK mismatch:\n  got:      %s\n  expected: %s", opIPKHex, expectedOpKey)
	}
}
