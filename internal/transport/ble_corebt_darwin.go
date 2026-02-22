// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package transport

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreBluetooth -framework Foundation

#import <CoreBluetooth/CoreBluetooth.h>
#import <Foundation/Foundation.h>

// Forward declaration of bt_queue (defined further below and in cbgo's bt.m).
// Functions that use it must see it declared before their first reference.
extern dispatch_queue_t bt_queue;

// ble_chr_is_notifying returns whether the given CBCharacteristic currently
// has notifications/indications enabled (i.e. the CCCD subscription is active).
static bool ble_chr_is_notifying(void *chr) {
	if (chr == NULL) return false;
	return ((CBCharacteristic *)chr).isNotifying;
}

// ble_chr_cached_value copies the characteristic's cached value into buf and
// returns the number of bytes copied. If the value is nil or empty, returns 0.
// buf must be at least buf_len bytes. The returned length is clamped to
// buf_len.
//
// The read is dispatched synchronously to bt_queue so it runs on the same
// serial queue that CoreBluetooth uses to update characteristic.value when
// an indication is received. Reading from an arbitrary goroutine thread
// would be a cross-thread property access and can silently miss updates.
static int ble_chr_cached_value(void *chr, void *buf, int buf_len) {
	if (chr == NULL) return 0;
	if (bt_queue == NULL) {
		// bt_queue unavailable (shouldn't happen in production) — read
		// directly as a best-effort fallback.
		NSData *fbVal = ((CBCharacteristic *)chr).value;
		if (fbVal == nil || fbVal.length == 0) return 0;
		int fn = (int)fbVal.length;
		if (fn > buf_len) fn = buf_len;
		memcpy(buf, fbVal.bytes, fn);
		return fn;
	}
	__block int n = 0;
	dispatch_sync(bt_queue, ^{
		NSData *qVal = ((CBCharacteristic *)chr).value;
		if (qVal == nil || qVal.length == 0) { return; }
		int qn = (int)qVal.length;
		if (qn > buf_len) qn = buf_len;
		memcpy(buf, qVal.bytes, qn);
		n = qn;
	});
	return n;
}

// ble_chr_clear_value sets the characteristic's cached value to nil so that
// subsequent polls can distinguish a new indication from a stale value.
// This uses KVC because CBCharacteristic.value is a readonly property.
// The write is dispatched to bt_queue for thread safety.
static void ble_chr_clear_value(void *chr) {
	if (chr == NULL) return;
	CBCharacteristic *clearChr = (CBCharacteristic *)chr;
	if (bt_queue == NULL) {
		@try { [clearChr setValue:nil forKey:@"value"]; }
		@catch (NSException *e) {}
		return;
	}
	dispatch_sync(bt_queue, ^{
		@try { [clearChr setValue:nil forKey:@"value"]; }
		@catch (NSException *e) {}
	});
}

// ble_chr_read_and_clear atomically reads the characteristic's cached value
// and clears it in a single dispatch_sync block on bt_queue. This prevents
// the race condition where a new indication arrives between a separate read
// and a separate clear. Returns the number of bytes copied into buf, or 0 if
// no value was available.
static int ble_chr_read_and_clear(void *chr, void *buf, int buf_len) {
	if (chr == NULL || bt_queue == NULL) return 0;
	CBCharacteristic *racChr = (CBCharacteristic *)chr;
	__block int n = 0;
	dispatch_sync(bt_queue, ^{
		NSData *racVal = racChr.value;
		if (racVal == nil || racVal.length == 0) { return; }
		int racN = (int)racVal.length;
		if (racN > buf_len) racN = buf_len;
		memcpy(buf, racVal.bytes, racN);
		n = racN;
		// Clear atomically with the read so the next poll won't re-deliver.
		@try { [racChr setValue:nil forKey:@"value"]; }
		@catch (NSException *e) {}
	});
	return n;
}

// bt_queue — the authoritative declaration (the extern above is a forward ref).
// bt_queue is the serial dispatch queue created by cbgo (tinygo-org/cbgo)
// for all CoreBluetooth operations. The CBCentralManager and its delegate
// are bound to this queue. All CoreBluetooth method calls that trigger
// delegate callbacks MUST be dispatched on this queue — calling them from
// an arbitrary Go goroutine thread can cause the operation to silently
// fail (the method returns, but the delegate callback never fires because
// CoreBluetooth internally requires dispatch context that only exists on
// the delegate queue).
//
// The symbol is defined in cbgo's bt.m and exported via bt.h as:
//   extern dispatch_queue_t bt_queue;
extern dispatch_queue_t bt_queue;

// ──────────────────────────────────────────────────────────────────────
// Fresh characteristic lookup
// ──────────────────────────────────────────────────────────────────────
//
// The CBCharacteristic pointer stored in cbgo's Characteristic struct can
// become "stale" — CoreBluetooth maintains its own authoritative list of
// characteristic objects inside CBService.characteristics, and the pointer
// that cbgo cached at discovery time may not be pointer-identical to the
// live object that CoreBluetooth recognises.
//
// When setNotifyValue:forCharacteristic: is called with a stale pointer,
// CoreBluetooth rejects it with:
//   CBErrorDomain Code=8 "The specified UUID is not allowed for this operation."
//
// The fix is to look up the characteristic by UUID from the service's live
// characteristics array immediately before every setNotifyValue: call.

// ble_find_fresh_characteristic looks up a CBCharacteristic with the same
// UUID as `stale` from `stale.service.characteristics`. Returns the live
// object, or stale itself if no match is found (best-effort fallback).
static CBCharacteristic * ble_find_fresh_characteristic(CBCharacteristic *stale) {
	if (stale == nil) return nil;
	CBService *svc = stale.service;
	if (svc == nil) return stale;
	CBUUID *targetUUID = stale.UUID;
	if (targetUUID == nil) return stale;

	for (CBCharacteristic *ch in svc.characteristics) {
		if ([ch.UUID isEqual:targetUUID]) {
			return ch;
		}
	}
	return stale;
}

// ble_chr_get_fresh_ptr looks up the live CBCharacteristic from
// svc.characteristics by UUID and returns its pointer. If no fresh
// match is found, returns the original pointer unchanged.
// Also sets *was_stale to true if the pointers differ.
static void * ble_chr_get_fresh_ptr(void *chr, bool *was_stale) {
	if (was_stale) *was_stale = false;
	if (chr == NULL) return chr;

	CBCharacteristic *stale = (CBCharacteristic *)chr;
	CBCharacteristic *fresh = ble_find_fresh_characteristic(stale);

	if (was_stale && fresh != stale) *was_stale = true;
	return (void *)fresh;
}

// ──────────────────────────────────────────────────────────────────────
// CCCD subscribe / unsubscribe helpers
// ──────────────────────────────────────────────────────────────────────

// ble_chr_unsubscribe calls [peripheral setNotifyValue:NO forCharacteristic:]
// on bt_queue to cancel any pending or broken subscription. This is used as
// the first step of the repair path in WaitForNotifying: if tinygo's wrong-
// thread setNotifyValue:YES left CoreBluetooth in a partially-processed state,
// a subsequent setNotifyValue:YES from bt_queue may be deduplicated as a no-op.
// Sending setNotifyValue:NO first forces CoreBluetooth to clear that state.
//
// The characteristic is looked up fresh from svc.characteristics to avoid
// the stale-pointer issue (CBError code 8).
static bool ble_chr_unsubscribe(void *chr) {
	if (chr == NULL || bt_queue == NULL) return false;
	CBCharacteristic *stale = (CBCharacteristic *)chr;
	CBService *svc = stale.service;
	if (svc == nil) return false;
	CBPeripheral *peripheral = svc.peripheral;
	if (peripheral == nil) return false;

	CBCharacteristic *fresh = ble_find_fresh_characteristic(stale);

	dispatch_sync(bt_queue, ^{
		[peripheral setNotifyValue:NO forCharacteristic:fresh];
	});
	return true;
}

// ble_chr_subscribe calls [peripheral setNotifyValue:YES forCharacteristic:]
// directly on the CBPeripheral that owns this characteristic, dispatched
// synchronously on bt_queue (cbgo's CoreBluetooth dispatch queue).
//
// The characteristic is looked up fresh from svc.characteristics by UUID
// to avoid the stale-pointer issue. cbgo caches CBCharacteristic pointers
// at discovery time, but CoreBluetooth maintains its own authoritative set
// of characteristic objects. If the cached pointer is not pointer-identical
// to the live object, setNotifyValue: fails with CBError code 8.
//
// Returns true if the call was made, false if any pointer in the chain
// (characteristic → service → peripheral) was nil or bt_queue is NULL.
static bool ble_chr_subscribe(void *chr) {
	if (chr == NULL || bt_queue == NULL) return false;
	CBCharacteristic *stale = (CBCharacteristic *)chr;
	CBService *svc = stale.service;
	if (svc == nil) return false;
	CBPeripheral *peripheral = svc.peripheral;
	if (peripheral == nil) return false;

	CBCharacteristic *fresh = ble_find_fresh_characteristic(stale);

	dispatch_sync(bt_queue, ^{
		[peripheral setNotifyValue:YES forCharacteristic:fresh];
	});
	return true;
}
*/
import "C"

import (
	"context"
	"fmt"
	"time"
	"unsafe"
)

// corebtIsNotifying checks whether the CBCharacteristic at the given raw
// pointer currently has notifications/indications enabled.
func corebtIsNotifying(chrPtr unsafe.Pointer) bool {
	return bool(C.ble_chr_is_notifying(chrPtr))
}

// corebtCachedValue returns a copy of the CBCharacteristic's cached value,
// read synchronously on bt_queue. Returns nil if the value is nil or empty.
func corebtCachedValue(chrPtr unsafe.Pointer) []byte {
	var buf [512]byte // BTP messages are at most ~247 bytes
	n := int(C.ble_chr_cached_value(chrPtr, unsafe.Pointer(&buf[0]), C.int(len(buf))))
	if n <= 0 {
		return nil
	}
	out := make([]byte, n)
	copy(out, buf[:n])
	return out
}

// corebtClearValue sets the characteristic's cached value to nil on bt_queue
// so that subsequent polls can detect a fresh indication.
func corebtClearValue(chrPtr unsafe.Pointer) {
	C.ble_chr_clear_value(chrPtr)
}

// corebtReadAndClear atomically reads and clears the characteristic's cached
// value on bt_queue. Returns nil if no value is available. This is the
// preferred way to consume indication data in a polling loop because it
// prevents the race between reading a value and clearing it (i.e. a new
// indication arriving in between).
func corebtReadAndClear(chrPtr unsafe.Pointer) []byte {
	var buf [512]byte
	n := int(C.ble_chr_read_and_clear(chrPtr, unsafe.Pointer(&buf[0]), C.int(len(buf))))
	if n <= 0 {
		return nil
	}
	out := make([]byte, n)
	copy(out, buf[:n])
	return out
}

// corebtSubscribe calls [peripheral setNotifyValue:YES forCharacteristic:]
// directly via CoreBluetooth, dispatched on cbgo's bt_queue. This is used
// by the repair path in WaitForNotifying when tinygo's async CCCD write
// fails silently. Returns true if the call was dispatched.
func corebtSubscribe(chrPtr unsafe.Pointer) bool {
	return bool(C.ble_chr_subscribe(chrPtr))
}

// corebtUnsubscribe calls [peripheral setNotifyValue:NO forCharacteristic:]
// on bt_queue to cancel any pending or broken subscription before
// re-subscribing cleanly.
func corebtUnsubscribe(chrPtr unsafe.Pointer) bool {
	return bool(C.ble_chr_unsubscribe(chrPtr))
}

// corebtGetFreshPtr looks up the live CBCharacteristic from
// svc.characteristics by UUID. If the cbgo-cached pointer is stale
// (not pointer-identical to the live object), it returns the fresh pointer
// and sets wasStale to true. This fresh pointer should be used for all
// subsequent CoreBluetooth operations (isNotifying checks, value polling).
func corebtGetFreshPtr(chrPtr unsafe.Pointer) (freshPtr unsafe.Pointer, wasStale bool) {
	var stale C.bool
	fresh := unsafe.Pointer(C.ble_chr_get_fresh_ptr(chrPtr, &stale))
	return fresh, bool(stale)
}

// ──────────────────────────────────────────────────────────────────────────────
// Polling helpers
// ──────────────────────────────────────────────────────────────────────────────

// corebtWaitNotifying polls the characteristic until isNotifying becomes true
// or the context is cancelled. This ensures the CCCD subscription is fully
// established before the caller sends any request that expects an indication
// response.
func corebtWaitNotifying(ctx context.Context, chrPtr unsafe.Pointer) error {
	const pollInterval = 10 * time.Millisecond
	for {
		if corebtIsNotifying(chrPtr) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ble: characteristic did not become notifying: %w", ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

// corebtPollValue polls the characteristic's cached value until it becomes
// non-nil or the context is cancelled. Returns the value data.
func corebtPollValue(ctx context.Context, chrPtr unsafe.Pointer) ([]byte, error) {
	const pollInterval = 10 * time.Millisecond
	for {
		if data := corebtCachedValue(chrPtr); data != nil {
			return data, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("ble: timed out waiting for characteristic value: %w", ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}
