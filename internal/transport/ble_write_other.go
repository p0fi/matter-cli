// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble && !darwin

package transport

import ble "tinygo.org/x/bluetooth"

// btCharWriteWithResponse performs the best available write-with-response on
// a characteristic. On non-Darwin builds in this configuration, tinygo's
// bluetooth API only exposes WriteWithoutResponse, so there is no available
// GATT write-with-response operation through this library version. We use
// WriteWithoutResponse as a best-effort fallback.
func btCharWriteWithResponse(c ble.DeviceCharacteristic, data []byte) (int, error) {
	return c.WriteWithoutResponse(data)
}
