// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble && darwin

package transport

import ble "tinygo.org/x/bluetooth"

// btCharWriteWithResponse performs a GATT Write Request (with ATT-level
// response) on a characteristic. On Darwin, tinygo exposes Write() for
// this purpose.
func btCharWriteWithResponse(c ble.DeviceCharacteristic, data []byte) (int, error) {
	return c.Write(data)
}
