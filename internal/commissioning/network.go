// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package commissioning

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

// NewEthernetCredentials creates NetworkCredentials for an Ethernet network
// (no credentials required).
func NewEthernetCredentials() NetworkCredentials {
	return NetworkCredentials{
		Type: NetworkEthernet,
	}
}
