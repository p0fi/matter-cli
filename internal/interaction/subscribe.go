// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

// SubscribeRequest is the TLV structure for a Subscribe Request message (opcode 0x03).
// It requests periodic attribute and/or event reports from the peer.
type SubscribeRequest struct {
	KeepSubscriptions  bool            `tlv:"0,bool"`
	MinIntervalFloor   uint16          `tlv:"1,uint"`
	MaxIntervalCeiling uint16          `tlv:"2,uint"`
	AttributeRequests  []AttributePath `tlv:"3,listarray"`
	EventRequests      []EventPath     `tlv:"4,listarray"`
	FabricFiltered     bool            `tlv:"7,bool"`
}

// SubscribeResponse is the TLV structure for a Subscribe Response message (opcode 0x04).
// It confirms subscription establishment and provides the negotiated parameters.
type SubscribeResponse struct {
	SubscriptionID uint32 `tlv:"0,uint"`
	MaxInterval    uint16 `tlv:"2,uint"`
}

// Subscription represents an active subscription to attribute or event changes.
// Reports are delivered through the Reports channel and errors through the
// Errors channel. The priming report is delivered as the first batch on
// Reports. Call Cancel to terminate the subscription.
type Subscription struct {
	// ID is the subscription identifier assigned by the peer.
	ID uint32
	// MaxInterval is the maximum reporting interval, in seconds, negotiated
	// with the peer in the SubscribeResponse.
	MaxInterval uint16
	// Reports delivers attribute report batches from the peer, starting with
	// the priming report.
	Reports <-chan []AttributeReport
	// Errors delivers errors that occur during the subscription, including a
	// non-success status for any attribute path (priming or ongoing).
	Errors <-chan error

	cancel func()
}

// Cancel terminates the subscription and closes its channels.
func (s *Subscription) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}
