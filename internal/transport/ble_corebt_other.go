// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble && !darwin

// This file provides no-op stubs for the CoreBluetooth helper functions
// defined in ble_corebt_darwin.go. On non-Darwin platforms (Linux, Windows)
// the raw CBCharacteristic pointer is never populated (extractCBCharacteristicPtr
// returns nil), so all code paths that call these functions are guarded by
// a "rawPtr == nil" check and will never reach these stubs at runtime.
// They exist solely to satisfy the Go compiler on non-Darwin builds.

package transport

import (
	"context"
	"fmt"
	"time"
	"unsafe"
)

// corebtIsNotifying always returns false on non-Darwin platforms.
// On these platforms rawPtr is always nil, so this is never called.
func corebtIsNotifying(_ unsafe.Pointer) bool { return false }

// corebtCachedValue always returns nil on non-Darwin platforms.
func corebtCachedValue(_ unsafe.Pointer) []byte { return nil }

// corebtClearValue is a no-op on non-Darwin platforms.
func corebtClearValue(_ unsafe.Pointer) {}

// corebtSubscribe always returns false on non-Darwin platforms.
func corebtSubscribe(_ unsafe.Pointer) bool { return false }

// corebtUnsubscribe always returns false on non-Darwin platforms.
func corebtUnsubscribe(_ unsafe.Pointer) bool { return false }

// corebtGetFreshPtr returns the pointer unchanged on non-Darwin platforms.
// wasStale is always false since there is no CoreBluetooth object graph to
// check against.
func corebtGetFreshPtr(chrPtr unsafe.Pointer) (unsafe.Pointer, bool) { return chrPtr, false }

// corebtWaitNotifying immediately returns a timeout error on non-Darwin
// platforms. Since rawPtr is always nil, WaitForNotifying returns before
// calling this function, so it is never reached in practice.
func corebtWaitNotifying(ctx context.Context, _ unsafe.Pointer) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("ble: characteristic did not become notifying: %w", ctx.Err())
	default:
		return nil
	}
}

// corebtPollValue blocks until the context is cancelled on non-Darwin
// platforms. Since rawPtr is always nil, WaitForValue returns before calling
// this function, so it is never reached in practice.
func corebtPollValue(ctx context.Context, _ unsafe.Pointer) ([]byte, error) {
	<-ctx.Done()
	return nil, fmt.Errorf("ble: timed out waiting for characteristic value: %w", ctx.Err())
}

// corebtReadAndClear always returns nil on non-Darwin platforms.
// rawPtr is always nil on these platforms, so this is never called in practice.
func corebtReadAndClear(_ unsafe.Pointer) []byte { return nil }

// corebtWriteWithoutResponse always returns -1 on non-Darwin platforms,
// signalling the caller to fall back to tinygo's write path.
// rawPtr is always nil on these platforms, so this is never called in practice.
func corebtWriteWithoutResponse(_ unsafe.Pointer, _ []byte) int { return -1 }

// corebtWriteWithResponse always returns -1 on non-Darwin platforms.
// rawPtr is always nil on these platforms, so this is never called in practice.
func corebtWriteWithResponse(_ unsafe.Pointer, _ []byte) int { return -1 }

// corebtIsBTQueueInitialized always returns false on non-Darwin platforms.
func corebtIsBTQueueInitialized() bool { return false }

// corebtCanSendWithoutResponse always returns true on non-Darwin platforms so
// callers proceed without waiting. rawPtr is always nil on these platforms.
func corebtCanSendWithoutResponse(_ unsafe.Pointer) bool { return true }

// corebtPeripheralIsConnected always returns true on non-Darwin platforms so
// the disconnect guard in corebtPollValue never fires. rawPtr is always nil.
func corebtPeripheralIsConnected(_ unsafe.Pointer) bool { return true }

// Ensure the time import is used (corebtWaitNotifying may need it in future).
var _ = time.Second
