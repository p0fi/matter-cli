// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package discovery provides mDNS-based device discovery for Matter devices.
// It can browse for both commissionable devices (advertising on _matterc._udp)
// and operational devices (advertising on _matter._tcp).
package discovery

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// CommissioningMode represents the commissioning state of a discovered device.
type CommissioningMode int

const (
	// CommissioningModeNone indicates the device is not in commissioning mode.
	CommissioningModeNone CommissioningMode = 0
	// CommissioningModeBasic indicates basic commissioning mode (passcode needed).
	CommissioningModeBasic CommissioningMode = 1
	// CommissioningModeEnhanced indicates enhanced commissioning mode.
	CommissioningModeEnhanced CommissioningMode = 2
)

// ServiceType identifies the mDNS service being browsed.
type ServiceType string

const (
	// ServiceCommissionable is the mDNS service type for commissionable Matter devices.
	ServiceCommissionable ServiceType = "_matterc._udp"
	// ServiceOperational is the mDNS service type for operational Matter devices.
	ServiceOperational ServiceType = "_matter._tcp"
)

// TransportType identifies the transport over which a device was discovered.
type TransportType string

const (
	// TransportIP indicates the device was discovered via mDNS/IP.
	TransportIP TransportType = "ip"
	// TransportBLE indicates the device was discovered via BLE.
	TransportBLE TransportType = "ble"
)

// Device represents a Matter device discovered via mDNS or BLE.
type Device struct {
	// Name is the mDNS instance name or BLE local name.
	Name string

	// Host is the mDNS hostname of the device (empty for BLE).
	Host string

	// IPs contains the IP addresses at which the device can be reached (empty for BLE).
	IPs []net.IP

	// Port is the Matter protocol port (0 for BLE).
	Port int

	// ServiceType indicates whether this is a commissionable or operational device.
	ServiceType ServiceType

	// Transport indicates whether the device was discovered via IP or BLE.
	Transport TransportType

	// Discriminator is the 12-bit device discriminator.
	Discriminator uint16

	// CommissioningMode is parsed from the CM= TXT record (IP only).
	CommissioningMode CommissioningMode

	// VendorID is the 16-bit vendor identifier.
	VendorID uint16

	// ProductID is the 16-bit product identifier.
	ProductID uint16

	// BLEAddress is the opaque BLE device address (non-empty only for BLE devices).
	BLEAddress string

	// SessionIdleInterval is parsed from the SII= TXT record (in milliseconds).
	SessionIdleInterval uint32

	// SessionActiveInterval is parsed from the SAI= TXT record (in milliseconds).
	SessionActiveInterval uint32

	// TXTRecords contains the raw TXT record key-value pairs (IP only).
	TXTRecords map[string]string
}

// parseTXTRecords populates Device fields from raw mDNS TXT record strings.
// Each entry is expected to be in "KEY=VALUE" format.
func (d *Device) parseTXTRecords(records []string) {
	d.TXTRecords = make(map[string]string, len(records))
	for _, r := range records {
		k, v, ok := strings.Cut(r, "=")
		if !ok {
			continue
		}
		d.TXTRecords[k] = v

		switch k {
		case "D":
			if n, err := strconv.ParseUint(v, 10, 16); err == nil {
				d.Discriminator = uint16(n)
			}
		case "CM":
			if n, err := strconv.ParseInt(v, 10, 32); err == nil {
				d.CommissioningMode = CommissioningMode(n)
			}
		case "VP":
			d.VendorID, d.ProductID = parseVP(v)
		case "SII":
			if n, err := strconv.ParseUint(v, 10, 32); err == nil {
				d.SessionIdleInterval = uint32(n)
			}
		case "SAI":
			if n, err := strconv.ParseUint(v, 10, 32); err == nil {
				d.SessionActiveInterval = uint32(n)
			}
		}
	}
}

// parseVP parses a vendor/product string of the form "VID+PID".
func parseVP(s string) (vendorID, productID uint16) {
	parts := strings.SplitN(s, "+", 2)
	if len(parts) < 1 {
		return 0, 0
	}
	if vid, err := strconv.ParseUint(parts[0], 10, 16); err == nil {
		vendorID = uint16(vid)
	}
	if len(parts) == 2 {
		if pid, err := strconv.ParseUint(parts[1], 10, 16); err == nil {
			productID = uint16(pid)
		}
	}
	return vendorID, productID
}

// String returns a human-readable summary of the discovered device.
func (d *Device) String() string {
	var addrs []string
	for _, ip := range d.IPs {
		addrs = append(addrs, ip.String())
	}
	return fmt.Sprintf("%s @ %s:%d (D=%d, CM=%d, VP=%d+%d)",
		d.Name, strings.Join(addrs, ","), d.Port,
		d.Discriminator, d.CommissioningMode, d.VendorID, d.ProductID)
}
