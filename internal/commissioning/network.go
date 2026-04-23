// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

import "fmt"

// NetworkType identifies the type of network for commissioning.
type NetworkType int

const (
	// NetworkEthernet is a wired Ethernet network (no credentials needed).
	NetworkEthernet NetworkType = iota
	// NetworkWiFi is a Wi-Fi network requiring SSID and password.
	NetworkWiFi
	// NetworkThread is a Thread network requiring an operational dataset.
	NetworkThread
)

// String returns a human-readable name for the network type.
func (t NetworkType) String() string {
	switch t {
	case NetworkEthernet:
		return "Ethernet"
	case NetworkWiFi:
		return "WiFi"
	case NetworkThread:
		return "Thread"
	default:
		return "Unknown"
	}
}

// WiFiCredentials holds the credentials needed to join a Wi-Fi network.
type WiFiCredentials struct {
	// SSID is the network name.
	SSID string
	// Password is the network password.
	Password string
}

// ThreadCredentials holds the dataset needed to join a Thread network.
type ThreadCredentials struct {
	// OperationalDataset is the Thread operational dataset as a raw byte slice.
	// This is the TLV-encoded dataset that includes network name, channel,
	// PAN ID, extended PAN ID, network key, and other parameters.
	OperationalDataset []byte
}

// NetworkCredentials holds network provisioning information for commissioning.
type NetworkCredentials struct {
	// Type indicates which network type these credentials are for.
	Type NetworkType
	// WiFi holds Wi-Fi credentials (non-nil only when Type == NetworkWiFi).
	WiFi *WiFiCredentials
	// Thread holds Thread credentials (non-nil only when Type == NetworkThread).
	Thread *ThreadCredentials
}

// NewWiFiCredentials creates NetworkCredentials for a Wi-Fi network.
func NewWiFiCredentials(ssid, password string) NetworkCredentials {
	return NetworkCredentials{
		Type: NetworkWiFi,
		WiFi: &WiFiCredentials{
			SSID:     ssid,
			Password: password,
		},
	}
}

// NewThreadCredentials creates NetworkCredentials for a Thread network.
func NewThreadCredentials(dataset []byte) NetworkCredentials {
	return NetworkCredentials{
		Type:   NetworkThread,
		Thread: &ThreadCredentials{OperationalDataset: dataset},
	}
}

// Thread operational dataset TLV type IDs (from the Thread specification).
// Each TLV is encoded as: 1-byte type | 1-byte length | value.
const (
	threadTLVActiveTimestamp = 0x0E // 8 bytes
	threadTLVChannel         = 0x00 // 3 bytes
	threadTLVChannelMask     = 0x35 // variable
	threadTLVExtendedPANID   = 0x02 // 8 bytes
	threadTLVMeshLocalPrefix = 0x07 // 8 bytes
	threadTLVNetworkKey      = 0x05 // 16 bytes
	threadTLVNetworkName     = 0x03 // 1-16 bytes
	threadTLVPANID           = 0x01 // 2 bytes
	threadTLVPSKc            = 0x04 // 16 bytes
	threadTLVSecurityPolicy  = 0x0C // 3-4 bytes
)

// minThreadDatasetLen is the minimum length of a valid Thread operational
// dataset. A dataset must contain at least: Active Timestamp (10), Channel (5),
// Extended PAN ID (10), Network Key (18), Network Name (3+), PAN ID (4),
// and several other fields. In practice the smallest valid datasets are ~80 bytes.
const minThreadDatasetLen = 50

// requiredThreadTLVTypes lists the TLV type IDs that must be present in a
// valid Thread Active Operational Dataset per the Thread specification.
var requiredThreadTLVTypes = []struct {
	id   byte
	name string
}{
	{threadTLVChannel, "Channel"},
	{threadTLVExtendedPANID, "Extended PAN ID"},
	{threadTLVNetworkKey, "Network Key"},
	{threadTLVNetworkName, "Network Name"},
	{threadTLVPANID, "PAN ID"},
}

// ValidateThreadDataset checks whether dataset looks like a valid Thread
// Active Operational Dataset. It verifies minimum length and the presence of
// required TLV type IDs. It does NOT validate individual TLV values.
func ValidateThreadDataset(dataset []byte) error {
	if len(dataset) < minThreadDatasetLen {
		return fmt.Errorf("Thread operational dataset is too short (%d bytes, minimum %d)\n"+
			"  A valid dataset is typically 100-200 bytes and can be obtained from your\n"+
			"  Thread border router, e.g.: ot-ctl dataset active -x",
			len(dataset), minThreadDatasetLen)
	}

	// Parse the dataset as Thread TLVs (type-length-value, 1 byte each for
	// type and length) and collect which type IDs are present.
	present := make(map[byte]bool)
	for i := 0; i < len(dataset); {
		if i+2 > len(dataset) {
			break // truncated TLV header
		}
		tlvType := dataset[i]
		tlvLen := int(dataset[i+1])
		i += 2 + tlvLen
		present[tlvType] = true
	}

	var missing []string
	for _, req := range requiredThreadTLVTypes {
		if !present[req.id] {
			missing = append(missing, req.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("Thread operational dataset is missing required fields: %v\n"+
			"  The dataset should be the hex-encoded output of: ot-ctl dataset active -x",
			missing)
	}

	return nil
}

// ExtractExtendedPANID parses the Thread operational dataset TLV and returns
// the 8-byte Extended PAN ID (type 0x02). This is the value that must be used
// as the NetworkID in ConnectNetwork commands for Thread devices.
func ExtractExtendedPANID(dataset []byte) ([]byte, error) {
	for i := 0; i < len(dataset); {
		if i+2 > len(dataset) {
			break // truncated TLV header
		}
		tlvType := dataset[i]
		tlvLen := int(dataset[i+1])
		i += 2
		if i+tlvLen > len(dataset) {
			return nil, fmt.Errorf("Thread dataset TLV truncated: type 0x%02X claims %d bytes but only %d remain",
				tlvType, tlvLen, len(dataset)-i)
		}
		if tlvType == threadTLVExtendedPANID {
			if tlvLen != 8 {
				return nil, fmt.Errorf("Thread Extended PAN ID has unexpected length %d (expected 8)", tlvLen)
			}
			extPanID := make([]byte, 8)
			copy(extPanID, dataset[i:i+8])
			return extPanID, nil
		}
		i += tlvLen
	}
	return nil, fmt.Errorf("Thread operational dataset does not contain an Extended PAN ID (type 0x%02X)", threadTLVExtendedPANID)
}

// NewEthernetCredentials creates NetworkCredentials for an Ethernet network
// (no credentials required).
func NewEthernetCredentials() NetworkCredentials {
	return NetworkCredentials{
		Type: NetworkEthernet,
	}
}
