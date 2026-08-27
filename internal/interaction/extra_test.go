// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/p0fi/matter-cli/internal/protocol"
)

// ---------------------------------------------------------------------------
// InvokeTimed
// ---------------------------------------------------------------------------

// TestClient_InvokeTimed verifies the timed-invoke path: TimedRequest →
// StatusResponse(Success) → InvokeRequest (with ACK piggybacked) →
// InvokeResponse.
func TestClient_InvokeTimed(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 20, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	type result struct {
		resp *InvokeResponseIB
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, err := client.InvokeTimed(ctx, session,
			NewCommandPath(1, 0x0006, 0x0001), nil, 5000)
		ch <- result{resp, err}
	}()

	// Give the goroutine time to send the TimedRequest and block on Receive.
	time.Sleep(60 * time.Millisecond)

	// Step 1 – acknowledge the TimedRequest with StatusResponse(Success).
	timedAck := StatusResponseMessage{Status: uint8(StatusSuccess)}
	injectResponse(t, em, session.ID, 0, OpcodeStatusResponse, timedAck)

	// Give the goroutine time to process the status response and send
	// InvokeRequest (which is a no-op since DefaultSendFunc is nil).
	time.Sleep(60 * time.Millisecond)

	// Step 2 – respond to the InvokeRequest.
	invokeResp := InvokeResponse{
		InvokeResponses: []InvokeResponseIB{
			{
				Status: &CommandStatusIB{
					Path:   NewCommandPath(1, 0x0006, 0x0001),
					Status: StatusIB{Status: uint8(StatusSuccess)},
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeInvokeResponse, invokeResp)

	r := <-ch
	if r.err != nil {
		t.Fatalf("InvokeTimed: %v", r.err)
	}
	if r.resp == nil {
		t.Fatal("response should not be nil")
	}
	if r.resp.Status == nil {
		t.Fatal("expected Status field in response")
	}
	if r.resp.Status.Status.Status != uint8(StatusSuccess) {
		t.Errorf("status = 0x%02X, want SUCCESS", r.resp.Status.Status.Status)
	}
}

// TestClient_InvokeTimed_Rejected verifies that when the device rejects the
// TimedRequest (responds with a non-success StatusResponse), InvokeTimed
// returns a StatusError.
func TestClient_InvokeTimed_Rejected(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 21, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		resp *InvokeResponseIB
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, err := client.InvokeTimed(ctx, session,
			NewCommandPath(1, 0x0101, 0x0000), nil, 1000)
		ch <- result{resp, err}
	}()

	time.Sleep(60 * time.Millisecond)

	// Device rejects the TimedRequest.
	rejection := StatusResponseMessage{Status: uint8(StatusBusy)}
	injectResponse(t, em, session.ID, 0, OpcodeStatusResponse, rejection)

	r := <-ch
	if r.err == nil {
		t.Fatal("expected error when TimedRequest is rejected, got nil")
	}
	if !IsStatus(r.err, StatusBusy) {
		t.Errorf("expected StatusBusy, got: %v", r.err)
	}
}

// TestClient_InvokeTimed_WithResponseData verifies that InvokeTimed correctly
// returns command response data (not just a status).
func TestClient_InvokeTimed_WithResponseData(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 22, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	type result struct {
		resp *InvokeResponseIB
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, err := client.InvokeTimed(ctx, session,
			NewCommandPath(0, 0x0030, 0x0000), []byte{0x09}, 2000)
		ch <- result{resp, err}
	}()

	time.Sleep(60 * time.Millisecond)

	timedAck := StatusResponseMessage{Status: uint8(StatusSuccess)}
	injectResponse(t, em, session.ID, 0, OpcodeStatusResponse, timedAck)

	time.Sleep(60 * time.Millisecond)

	respFields := []byte{0x04, 0x00, 0x04, 0x01}
	invokeResp := InvokeResponse{
		InvokeResponses: []InvokeResponseIB{
			{
				Command: &CommandDataIB{
					Path:   NewCommandPath(0, 0x0030, 0x0001),
					Fields: respFields,
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeInvokeResponse, invokeResp)

	r := <-ch
	if r.err != nil {
		t.Fatalf("InvokeTimed: %v", r.err)
	}
	if r.resp.Command == nil {
		t.Fatal("expected Command in response")
	}
	if r.resp.Command.Path.ClusterID != 0x0030 {
		t.Errorf("ClusterID = 0x%04X, want 0x0030", r.resp.Command.Path.ClusterID)
	}
}

// ---------------------------------------------------------------------------
// Subscribe – ongoing report error paths
// ---------------------------------------------------------------------------

// establishSubscription is a helper that sets up a subscription and returns
// the active Subscription.  It handles the priming report + SubscribeResponse
// exchange that Subscribe requires before returning.
func establishSubscription(t *testing.T, em *protocol.ExchangeManager, session *protocol.Session, client *Client) *Subscription {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

	type result struct {
		sub *Subscription
		err error
	}
	ch := make(chan result, 1)

	go func() {
		sub, err := client.Subscribe(ctx, session,
			[]AttributePath{NewAttributePath(1, 0x0006, 0x0000)},
			1, 60,
		)
		ch <- result{sub, err}
	}()

	time.Sleep(60 * time.Millisecond)

	// Priming report.
	primingReport := ReportData{
		AttributeReports: []AttributeReport{
			{
				Data: &AttributeData{
					DataVersion: 1,
					Path:        NewAttributePath(1, 0x0006, 0x0000),
					Data:        []byte{0x09},
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeReportData, primingReport)

	time.Sleep(60 * time.Millisecond)

	// SubscribeResponse.
	subResp := SubscribeResponse{
		SubscriptionID: 77,
		MaxInterval:    30,
	}
	injectResponse(t, em, session.ID, 0, OpcodeSubscribeResponse, subResp)

	r := <-ch
	if r.err != nil {
		cancel()
		t.Fatalf("Subscribe setup failed: %v", r.err)
	}

	// Store cancel so we can clean up in the test; the subscription itself
	// holds its own cancel that will be called by sub.Cancel().
	_ = cancel

	// Drain the priming report so callers can assume the next Reports batch
	// is the first ongoing report.
	select {
	case <-r.sub.Reports:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out draining priming report")
	}

	return r.sub
}

// TestClient_Subscribe_OngoingErrorStatus verifies that an error-status
// StatusResponse received during an active subscription is forwarded on the
// Errors channel and terminates the goroutine.
func TestClient_Subscribe_OngoingErrorStatus(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 30, Type: protocol.SessionCASE}
	client := NewClient(em)

	sub := establishSubscription(t, em, session, client)
	defer sub.Cancel()

	time.Sleep(60 * time.Millisecond)

	// Inject an error StatusResponse into the ongoing subscription exchange.
	errStatus := StatusResponseMessage{Status: uint8(StatusTimeout)}
	injectResponse(t, em, session.ID, 0, OpcodeStatusResponse, errStatus)

	select {
	case err, ok := <-sub.Errors:
		if !ok {
			t.Fatal("Errors channel closed without delivering an error")
		}
		if err == nil {
			t.Fatal("expected a non-nil error on Errors channel")
		}
		if !IsStatus(err, StatusTimeout) {
			t.Errorf("expected StatusTimeout, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error on Errors channel")
	}
}

// TestClient_Subscribe_OngoingAttributeStatusIsError verifies that a
// per-attribute AttributeReport.Status failure carried inside an otherwise
// well-formed ongoing ReportData is surfaced on Errors (not silently dropped
// or forwarded as if it were data) and terminates the goroutine.
func TestClient_Subscribe_OngoingAttributeStatusIsError(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 35, Type: protocol.SessionCASE}
	client := NewClient(em)

	sub := establishSubscription(t, em, session, client)
	defer sub.Cancel()

	time.Sleep(60 * time.Millisecond)

	report := ReportData{
		AttributeReports: []AttributeReport{
			{
				Status: &AttributeStatus{
					Path:   NewAttributePath(1, 0x0006, 0x0000),
					Status: StatusIB{Status: uint8(StatusUnsupportedAttribute)},
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeReportData, report)

	select {
	case reports := <-sub.Reports:
		t.Fatalf("expected no data report, got: %+v", reports)
	case err, ok := <-sub.Errors:
		if !ok {
			t.Fatal("Errors channel closed without delivering an error")
		}
		if !IsStatus(err, StatusUnsupportedAttribute) {
			t.Errorf("expected StatusUnsupportedAttribute, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for attribute status error")
	}
}

// TestClient_Subscribe_OngoingSuccessStatus verifies that a success StatusResponse
// during an active subscription is silently ignored (the subscription continues).
func TestClient_Subscribe_OngoingSuccessStatus(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 31, Type: protocol.SessionCASE}
	client := NewClient(em)

	sub := establishSubscription(t, em, session, client)
	defer sub.Cancel()

	time.Sleep(60 * time.Millisecond)

	// Inject a success StatusResponse – this should be silently ignored.
	successStatus := StatusResponseMessage{Status: uint8(StatusSuccess)}
	injectResponse(t, em, session.ID, 0, OpcodeStatusResponse, successStatus)

	// Give the goroutine time to process the success status and loop back.
	time.Sleep(60 * time.Millisecond)

	// Now inject a real report to confirm the subscription is still alive.
	periodicReport := ReportData{
		AttributeReports: []AttributeReport{
			{
				Data: &AttributeData{
					DataVersion: 2,
					Path:        NewAttributePath(1, 0x0006, 0x0000),
					Data:        []byte{0x08},
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeReportData, periodicReport)

	select {
	case reports, ok := <-sub.Reports:
		if !ok {
			t.Fatal("Reports channel closed unexpectedly")
		}
		if len(reports) != 1 {
			t.Fatalf("expected 1 report, got %d", len(reports))
		}
		if reports[0].Data == nil {
			t.Fatal("report Data should not be nil")
		}
		if reports[0].Data.DataVersion != 2 {
			t.Errorf("DataVersion = %d, want 2", reports[0].Data.DataVersion)
		}
	case err := <-sub.Errors:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for periodic report after success status")
	}
}

// TestClient_Subscribe_UnknownOpcodeIgnored verifies that a message with an
// opcode that is neither StatusResponse nor ReportData is silently dropped and
// the subscription continues.
func TestClient_Subscribe_UnknownOpcodeIgnored(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 32, Type: protocol.SessionCASE}
	client := NewClient(em)

	sub := establishSubscription(t, em, session, client)
	defer sub.Cancel()

	time.Sleep(60 * time.Millisecond)

	// Inject a message with opcode 0xFF (unknown) – should be silently ignored.
	unknownMsg := StatusResponseMessage{Status: 0} // payload doesn't matter
	injectResponse(t, em, session.ID, 0, 0xFF, unknownMsg)

	// Give the goroutine time to drop the unknown message.
	time.Sleep(60 * time.Millisecond)

	// Subscription should still be alive; inject a real report.
	periodicReport := ReportData{
		AttributeReports: []AttributeReport{
			{
				Data: &AttributeData{
					DataVersion: 3,
					Path:        NewAttributePath(1, 0x0006, 0x0000),
					Data:        []byte{0x09},
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeReportData, periodicReport)

	select {
	case reports, ok := <-sub.Reports:
		if !ok {
			t.Fatal("Reports channel closed unexpectedly")
		}
		if len(reports) != 1 {
			t.Fatalf("expected 1 report, got %d", len(reports))
		}
		if reports[0].Data.DataVersion != 3 {
			t.Errorf("DataVersion = %d, want 3", reports[0].Data.DataVersion)
		}
	case err := <-sub.Errors:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for report after unknown-opcode message")
	}
}

// TestClient_Subscribe_MultiplePeriodicReports verifies that multiple periodic
// reports are all delivered in order through the Reports channel.
func TestClient_Subscribe_MultiplePeriodicReports(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 33, Type: protocol.SessionCASE}
	client := NewClient(em)

	sub := establishSubscription(t, em, session, client)
	defer sub.Cancel()

	const numReports = 3
	for i := 0; i < numReports; i++ {
		time.Sleep(40 * time.Millisecond)

		report := ReportData{
			AttributeReports: []AttributeReport{
				{
					Data: &AttributeData{
						DataVersion: uint32(10 + i),
						Path:        NewAttributePath(1, 0x0006, 0x0000),
						Data:        []byte{0x09},
					},
				},
			},
		}
		injectResponse(t, em, session.ID, 0, OpcodeReportData, report)

		select {
		case reports, ok := <-sub.Reports:
			if !ok {
				t.Fatalf("report %d: Reports channel closed", i)
			}
			if len(reports) != 1 {
				t.Fatalf("report %d: expected 1 report, got %d", i, len(reports))
			}
			wantVersion := uint32(10 + i)
			if reports[0].Data.DataVersion != wantVersion {
				t.Errorf("report %d: DataVersion = %d, want %d",
					i, reports[0].Data.DataVersion, wantVersion)
			}
		case err := <-sub.Errors:
			t.Fatalf("report %d: unexpected error: %v", i, err)
		case <-time.After(2 * time.Second):
			t.Fatalf("report %d: timed out", i)
		}
	}
}

// TestClient_Subscribe_CancelDrainsChannels verifies that calling Cancel
// causes both the Reports and Errors channels to be closed.
func TestClient_Subscribe_CancelDrainsChannels(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 34, Type: protocol.SessionCASE}
	client := NewClient(em)

	sub := establishSubscription(t, em, session, client)

	sub.Cancel()

	// After cancel the goroutine should return and close both channels.
	// Allow a short time for the goroutine to finish.
	deadline := time.After(2 * time.Second)

	reportsClosed := false
	errorsClosed := false

	for !reportsClosed || !errorsClosed {
		select {
		case _, ok := <-sub.Reports:
			if !ok {
				reportsClosed = true
			}
		case _, ok := <-sub.Errors:
			if !ok {
				errorsClosed = true
			}
		case <-deadline:
			t.Fatalf("timed out: reportsClosed=%v errorsClosed=%v",
				reportsClosed, errorsClosed)
		}
	}
}

// ---------------------------------------------------------------------------
// sendIMMessageWithACK – ACK piggybacking
// ---------------------------------------------------------------------------

// TestSendIMMessageWithACK_WithACK verifies that when ack=true the resulting
// message has ExFlagACK set and carries the expected AckMessageCounter.
// We exercise this indirectly through InvokeTimed, but here we verify the
// exchange-level header flags are wired correctly by inspecting a captured
// outbound message.
func TestSendIMMessageWithACK_WithACK(t *testing.T) {
	// Capture the message that goes out on the wire.
	var captured *protocol.Message
	var capturedMu sync.Mutex
	em := protocol.NewExchangeManager()
	em.DefaultSendFunc = func(_ context.Context, msg *protocol.Message) error {
		capturedMu.Lock()
		captured = msg
		capturedMu.Unlock()
		return nil
	}

	session := &protocol.Session{ID: 40, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	type result struct {
		resp *InvokeResponseIB
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, err := client.InvokeTimed(ctx, session,
			NewCommandPath(1, 0x0006, 0x0002), nil, 3000)
		ch <- result{resp, err}
	}()

	time.Sleep(60 * time.Millisecond)

	// Inject StatusResponse with a known MessageCounter so we can assert the
	// ACK counter value on the subsequent InvokeRequest.
	timedAckMsg := buildIMMessage(session.ID, 0, OpcodeStatusResponse, StatusResponseMessage{Status: uint8(StatusSuccess)})
	timedAckMsg.Header.MessageCounter = 0xDEAD
	if err := em.HandleMessage(context.Background(), timedAckMsg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	time.Sleep(60 * time.Millisecond)

	// The second message sent (InvokeRequest) should have ExFlagACK set and
	// AckMessageCounter == 0xDEAD.  captured is set by DefaultSendFunc.
	capturedMu.Lock()
	capturedMsg := captured
	capturedMu.Unlock()
	if capturedMsg == nil {
		t.Fatal("no message was captured by DefaultSendFunc")
	}
	if capturedMsg.Protocol.ExchangeFlags&protocol.ExFlagACK == 0 {
		t.Errorf("expected ExFlagACK to be set on InvokeRequest, flags=0x%02X",
			capturedMsg.Protocol.ExchangeFlags)
	}
	if capturedMsg.Protocol.AckMessageCounter != 0xDEAD {
		t.Errorf("AckMessageCounter = 0x%04X, want 0xDEAD",
			capturedMsg.Protocol.AckMessageCounter)
	}

	// Now deliver an InvokeResponse so the goroutine can finish.
	invokeResp := InvokeResponse{
		InvokeResponses: []InvokeResponseIB{
			{
				Status: &CommandStatusIB{
					Path:   NewCommandPath(1, 0x0006, 0x0002),
					Status: StatusIB{Status: uint8(StatusSuccess)},
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeInvokeResponse, invokeResp)

	r := <-ch
	if r.err != nil {
		t.Fatalf("InvokeTimed: %v", r.err)
	}
}

// ---------------------------------------------------------------------------
// Write – additional paths
// ---------------------------------------------------------------------------

// TestClient_Write_MultipleAttributes verifies that a WriteRequest carrying
// more than one attribute write is correctly handled.
func TestClient_Write_MultipleAttributes(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 50, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		statuses []AttributeStatus
		err      error
	}
	ch := make(chan result, 1)

	go func() {
		statuses, err := client.Write(ctx, session,
			AttributeWrite{
				Path: NewAttributePath(1, 0x0008, 0x0001),
				Data: []byte{0x04, 0xFE},
			},
			AttributeWrite{
				Path: NewAttributePath(1, 0x0008, 0x0011),
				Data: []byte{0x04, 0x0A},
			},
		)
		ch <- result{statuses, err}
	}()

	time.Sleep(60 * time.Millisecond)

	writeResp := WriteResponse{
		WriteResponses: []AttributeStatus{
			{
				Path:   NewAttributePath(1, 0x0008, 0x0001),
				Status: StatusIB{Status: uint8(StatusSuccess)},
			},
			{
				Path:   NewAttributePath(1, 0x0008, 0x0011),
				Status: StatusIB{Status: uint8(StatusSuccess)},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeWriteResponse, writeResp)

	r := <-ch
	if r.err != nil {
		t.Fatalf("Write: %v", r.err)
	}
	if len(r.statuses) != 2 {
		t.Fatalf("statuses len = %d, want 2", len(r.statuses))
	}
	for i, s := range r.statuses {
		if s.Status.Status != uint8(StatusSuccess) {
			t.Errorf("status[%d] = 0x%02X, want SUCCESS", i, s.Status.Status)
		}
	}
}

// TestClient_Read_WildcardPath verifies that a Read with a wildcard-endpoint
// AttributePath (all pointer fields nil) marshals and routes correctly.
func TestClient_Read_WildcardPath(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 51, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		reports []AttributeReport
		err     error
	}
	ch := make(chan result, 1)

	// Wildcard path – only ClusterID and AttributeID set, EndpointID nil.
	clID := uint32(0x0006)
	atID := uint32(0x0000)
	wildcardPath := AttributePath{
		ClusterID:   &clID,
		AttributeID: &atID,
	}

	go func() {
		reports, err := client.Read(ctx, session, wildcardPath)
		ch <- result{reports, err}
	}()

	time.Sleep(60 * time.Millisecond)

	reportData := ReportData{
		AttributeReports: []AttributeReport{
			{
				Data: &AttributeData{
					DataVersion: 5,
					Path:        NewAttributePath(0, 0x0006, 0x0000),
					Data:        []byte{0x09},
				},
			},
			{
				Data: &AttributeData{
					DataVersion: 5,
					Path:        NewAttributePath(1, 0x0006, 0x0000),
					Data:        []byte{0x08},
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeReportData, reportData)

	r := <-ch
	if r.err != nil {
		t.Fatalf("Read wildcard: %v", r.err)
	}
	if len(r.reports) != 2 {
		t.Fatalf("reports len = %d, want 2", len(r.reports))
	}
}

// TestClient_Read_AttributeErrorInReport verifies that a ReportData carrying
// an AttributeStatus (error) instead of AttributeData is parsed correctly and
// returned in the reports slice.
func TestClient_Read_AttributeErrorInReport(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 52, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		reports []AttributeReport
		err     error
	}
	ch := make(chan result, 1)

	go func() {
		reports, err := client.Read(ctx, session, NewAttributePath(0, 0x0300, 0xFFFF))
		ch <- result{reports, err}
	}()

	time.Sleep(60 * time.Millisecond)

	// Respond with an attribute-level error (unsupported attribute).
	reportData := ReportData{
		AttributeReports: []AttributeReport{
			{
				Status: &AttributeStatus{
					Path: NewAttributePath(0, 0x0300, 0xFFFF),
					Status: StatusIB{
						Status: uint8(StatusUnsupportedAttribute),
					},
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeReportData, reportData)

	r := <-ch
	if r.err != nil {
		t.Fatalf("Read: %v", r.err)
	}
	if len(r.reports) != 1 {
		t.Fatalf("reports len = %d, want 1", len(r.reports))
	}
	if r.reports[0].Status == nil {
		t.Fatal("expected Status (error) in report, got nil")
	}
	if r.reports[0].Status.Status.Status != uint8(StatusUnsupportedAttribute) {
		t.Errorf("status = 0x%02X, want UNSUPPORTED_ATTRIBUTE",
			r.reports[0].Status.Status.Status)
	}
	if r.reports[0].Data != nil {
		t.Error("Data should be nil when Status is set")
	}
}

// ---------------------------------------------------------------------------
// Read / Write – unexpected opcode paths
// ---------------------------------------------------------------------------

// TestClient_Read_UnexpectedOpcode verifies that Read returns an error when it
// receives a message with an opcode other than ReportData or StatusResponse.
func TestClient_Read_UnexpectedOpcode(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 60, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		reports []AttributeReport
		err     error
	}
	ch := make(chan result, 1)

	go func() {
		reports, err := client.Read(ctx, session, NewAttributePath(1, 0x0006, 0x0000))
		ch <- result{reports, err}
	}()

	time.Sleep(60 * time.Millisecond)

	// Inject a WriteResponse instead of a ReportData.
	injectResponse(t, em, session.ID, 0, OpcodeWriteResponse, WriteResponse{})

	r := <-ch
	if r.err == nil {
		t.Fatal("expected error for unexpected opcode, got nil")
	}
	t.Logf("correctly failed with: %v", r.err)
}

// TestClient_Write_UnexpectedOpcode verifies that Write returns an error when
// the response opcode is not WriteResponse.
func TestClient_Write_UnexpectedOpcode(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 61, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		statuses []AttributeStatus
		err      error
	}
	ch := make(chan result, 1)

	go func() {
		statuses, err := client.Write(ctx, session,
			AttributeWrite{
				Path: NewAttributePath(1, 0x0006, 0x0000),
				Data: []byte{0x09},
			},
		)
		ch <- result{statuses, err}
	}()

	time.Sleep(60 * time.Millisecond)

	// Inject a ReportData instead of a WriteResponse.
	injectResponse(t, em, session.ID, 0, OpcodeReportData, ReportData{})

	r := <-ch
	if r.err == nil {
		t.Fatal("expected error for unexpected opcode, got nil")
	}
	t.Logf("correctly failed with: %v", r.err)
}

// TestClient_Invoke_UnexpectedOpcode verifies that Invoke returns an error
// when the response opcode is not InvokeResponse.
func TestClient_Invoke_UnexpectedOpcode(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 62, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		resp *InvokeResponseIB
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, err := client.Invoke(ctx, session, NewCommandPath(1, 0x0006, 0x0001), nil)
		ch <- result{resp, err}
	}()

	time.Sleep(60 * time.Millisecond)

	// Inject a ReportData instead of an InvokeResponse.
	injectResponse(t, em, session.ID, 0, OpcodeReportData, ReportData{})

	r := <-ch
	if r.err == nil {
		t.Fatal("expected error for unexpected opcode, got nil")
	}
	t.Logf("correctly failed with: %v", r.err)
}

// TestClient_Subscribe_UnexpectedSubscribeOpcode verifies that Subscribe
// returns an error when the SubscribeResponse opcode is replaced by something
// unexpected.
func TestClient_Subscribe_UnexpectedSubscribeOpcode(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 63, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		sub *Subscription
		err error
	}
	ch := make(chan result, 1)

	go func() {
		sub, err := client.Subscribe(ctx, session,
			[]AttributePath{NewAttributePath(1, 0x0006, 0x0000)},
			1, 60,
		)
		ch <- result{sub, err}
	}()

	time.Sleep(60 * time.Millisecond)

	// Send the priming report correctly.
	primingReport := ReportData{
		AttributeReports: []AttributeReport{
			{
				Data: &AttributeData{
					DataVersion: 1,
					Path:        NewAttributePath(1, 0x0006, 0x0000),
					Data:        []byte{0x09},
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeReportData, primingReport)

	time.Sleep(60 * time.Millisecond)

	// Send a ReportData instead of a SubscribeResponse.
	injectResponse(t, em, session.ID, 0, OpcodeReportData, ReportData{})

	r := <-ch
	if r.err == nil {
		t.Fatal("expected error when SubscribeResponse opcode is wrong, got nil")
	}
	t.Logf("correctly failed with: %v", r.err)
}

// TestClient_Invoke_EmptyResponse verifies that Invoke returns an error when
// the InvokeResponse contains no results.
func TestClient_Invoke_EmptyResponse(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 64, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		resp *InvokeResponseIB
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, err := client.Invoke(ctx, session, NewCommandPath(1, 0x0006, 0x0001), nil)
		ch <- result{resp, err}
	}()

	time.Sleep(60 * time.Millisecond)

	// Empty InvokeResponse – no InvokeResponses entries.
	injectResponse(t, em, session.ID, 0, OpcodeInvokeResponse, InvokeResponse{})

	r := <-ch
	if r.err == nil {
		t.Fatal("expected error for empty invoke response, got nil")
	}
	t.Logf("correctly failed with: %v", r.err)
}
