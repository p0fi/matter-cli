// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble && !darwin

package transport

import ble "tinygo.org/x/bluetooth"

// btCharWriteWithResponse performs the best available write-with-response on
// a characteristic. On Linux and Windows, tinygo's bluetooth only exposes
// WriteWithoutResponse — there is no GATT Write Request available through the
// BlueZ D-Bus API in this library version. We use WriteWithoutResponse as a
// best-effort fallback.
func btCharWriteWithResponse(c ble.DeviceCharacteristic, data []byte) (int, error) {
	return c.WriteWithoutResponse(data)
}
