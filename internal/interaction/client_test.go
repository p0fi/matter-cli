// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import (
	"context"
	"testing"
	"time"

	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/tlv"
)

// buildIMMessage creates a protocol.Message with IM protocol headers and
// TLV-encoded payload suitable for injecting into an exchange.
func buildIMMessage(sessionID uint16, exchangeID uint16, opcode byte, payload any) *protocol.Message {
	data, err := tlv.Marshal(payload)
	if err != nil {
		panic("buildIMMessage: " + err.Error())
	}
	return &protocol.Message{
		Header: protocol.MessageHeader{
			SessionID: sessionID,
		},
		Protocol: protocol.ProtocolHeader{
			ProtocolOpcode: opcode,
			ProtocolID:     ProtocolID,
			ExchangeID:     exchangeID,
			ExchangeFlags:  0, // response (not initiator)
		},
		Payload: data,
	}
}

// injectResponse sends a response message to an exchange via HandleMessage.
// The message is routed as a non-initiator response to the locally-created exchange.
func injectResponse(t *testing.T, em *protocol.ExchangeManager, sessionID uint16, exchangeID uint16, opcode byte, payload any) {
	t.Helper()
	msg := buildIMMessage(sessionID, exchangeID, opcode, payload)
	if err := em.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("injectResponse: %v", err)
	}
}

func TestClient_Read_SingleAttribute(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 1, Type: protocol.SessionPASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run Read in background; inject response.
	type result struct {
		reports []AttributeReport
		err     error
	}
	ch := make(chan result, 1)

	go func() {
		reports, err := client.Read(ctx, session, NewAttributePath(1, 0x0006, 0x0000))
		ch <- result{reports, err}
	}()

	// Give the goroutine time to create the exchange and start receiving.
	time.Sleep(50 * time.Millisecond)

	reportData := ReportData{
		AttributeReports: []AttributeReport{
			{
				Data: &AttributeData{
					DataVersion: 1,
					Path:        NewAttributePath(1, 0x0006, 0x0000),
					Data:        []byte{0x09}, // TLV true
				},
			},
		},
	}

	// Exchange ID 0 (first exchange created by the manager).
	injectResponse(t, em, session.ID, 0, OpcodeReportData, reportData)

	r := <-ch
	if r.err != nil {
		t.Fatalf("Read: %v", r.err)
	}
	if len(r.reports) != 1 {
		t.Fatalf("reports len = %d, want 1", len(r.reports))
	}
	if r.reports[0].Data == nil {
		t.Fatal("report Data should not be nil")
	}
	if r.reports[0].Data.DataVersion != 1 {
		t.Errorf("DataVersion = %d, want 1", r.reports[0].Data.DataVersion)
	}
}

func TestClient_Read_ChunkedResponse(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 2, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		reports []AttributeReport
		err     error
	}
	ch := make(chan result, 1)

	go func() {
		reports, err := client.Read(ctx, session,
			NewAttributePath(1, 0x0006, 0x0000),
			NewAttributePath(1, 0x0008, 0x0000),
		)
		ch <- result{reports, err}
	}()

	time.Sleep(50 * time.Millisecond)

	// First chunk with MoreChunkedMessages=true.
	more := true
	chunk1 := ReportData{
		MoreChunkedMessages: &more,
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
	injectResponse(t, em, session.ID, 0, OpcodeReportData, chunk1)

	// Give client time to process and send ack.
	time.Sleep(50 * time.Millisecond)

	// Second chunk (final).
	chunk2 := ReportData{
		AttributeReports: []AttributeReport{
			{
				Data: &AttributeData{
					DataVersion: 1,
					Path:        NewAttributePath(1, 0x0008, 0x0000),
					Data:        []byte{0x04, 0x80},
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeReportData, chunk2)

	r := <-ch
	if r.err != nil {
		t.Fatalf("Read: %v", r.err)
	}
	if len(r.reports) != 2 {
		t.Fatalf("reports len = %d, want 2", len(r.reports))
	}
}

func TestClient_Read_StatusError(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 3, Type: protocol.SessionCASE}
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

	time.Sleep(50 * time.Millisecond)

	// Respond with a StatusResponse error instead of ReportData.
	statusResp := StatusResponseMessage{
		Status: uint8(StatusBusy),
	}
	injectResponse(t, em, session.ID, 0, OpcodeStatusResponse, statusResp)

	r := <-ch
	if r.err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsStatus(r.err, StatusBusy) {
		t.Errorf("expected StatusBusy, got: %v", r.err)
	}
}

func TestClient_Write(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 4, Type: protocol.SessionCASE}
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
				Path: NewAttributePath(1, 0x0008, 0x0000),
				Data: []byte{0x04, 0x80},
			},
		)
		ch <- result{statuses, err}
	}()

	time.Sleep(50 * time.Millisecond)

	writeResp := WriteResponse{
		WriteResponses: []AttributeStatus{
			{
				Path:   NewAttributePath(1, 0x0008, 0x0000),
				Status: StatusIB{Status: uint8(StatusSuccess)},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeWriteResponse, writeResp)

	r := <-ch
	if r.err != nil {
		t.Fatalf("Write: %v", r.err)
	}
	if len(r.statuses) != 1 {
		t.Fatalf("statuses len = %d, want 1", len(r.statuses))
	}
	if r.statuses[0].Status.Status != uint8(StatusSuccess) {
		t.Errorf("write status = 0x%02X, want SUCCESS", r.statuses[0].Status.Status)
	}
}

func TestClient_Write_Error(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 5, Type: protocol.SessionCASE}
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

	time.Sleep(50 * time.Millisecond)

	statusResp := StatusResponseMessage{
		Status: uint8(StatusUnsupportedWrite),
	}
	injectResponse(t, em, session.ID, 0, OpcodeStatusResponse, statusResp)

	r := <-ch
	if r.err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsStatus(r.err, StatusUnsupportedWrite) {
		t.Errorf("expected StatusUnsupportedWrite, got: %v", r.err)
	}
}

func TestClient_Invoke_SuccessWithStatus(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 6, Type: protocol.SessionCASE}
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

	time.Sleep(50 * time.Millisecond)

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
		t.Fatalf("Invoke: %v", r.err)
	}
	if r.resp.Status == nil {
		t.Fatal("expected Status in response")
	}
	if r.resp.Status.Status.Status != uint8(StatusSuccess) {
		t.Errorf("status = 0x%02X, want SUCCESS", r.resp.Status.Status.Status)
	}
}

func TestClient_Invoke_WithResponseData(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 7, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		resp *InvokeResponseIB
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, err := client.Invoke(ctx, session, NewCommandPath(0, 0x0030, 0x0000), []byte{0x09})
		ch <- result{resp, err}
	}()

	time.Sleep(50 * time.Millisecond)

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
		t.Fatalf("Invoke: %v", r.err)
	}
	if r.resp.Command == nil {
		t.Fatal("expected Command in response")
	}
	if r.resp.Command.Path.ClusterID != 0x0030 {
		t.Errorf("ClusterID = 0x%04X, want 0x0030", r.resp.Command.Path.ClusterID)
	}
	if len(r.resp.Command.Fields) != len(respFields) {
		t.Errorf("Fields len = %d, want %d", len(r.resp.Command.Fields), len(respFields))
	}
}

func TestClient_Invoke_Error(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 8, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	type result struct {
		resp *InvokeResponseIB
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, err := client.Invoke(ctx, session, NewCommandPath(1, 0x0006, 0x0099), nil)
		ch <- result{resp, err}
	}()

	time.Sleep(50 * time.Millisecond)

	statusResp := StatusResponseMessage{
		Status: uint8(StatusUnsupportedCommand),
	}
	injectResponse(t, em, session.ID, 0, OpcodeStatusResponse, statusResp)

	r := <-ch
	if r.err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsStatus(r.err, StatusUnsupportedCommand) {
		t.Errorf("expected StatusUnsupportedCommand, got: %v", r.err)
	}
}

func TestClient_Subscribe(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 9, Type: protocol.SessionCASE}
	client := NewClient(em)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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

	time.Sleep(50 * time.Millisecond)

	// 1. Send priming ReportData.
	primingReport := ReportData{
		AttributeReports: []AttributeReport{
			{
				Data: &AttributeData{
					DataVersion: 1,
					Path:        NewAttributePath(1, 0x0006, 0x0000),
					Data:        []byte{0x08}, // TLV false
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeReportData, primingReport)

	time.Sleep(50 * time.Millisecond)

	// 2. Send SubscribeResponse.
	subResp := SubscribeResponse{
		SubscriptionID: 42,
		MaxInterval:    30,
	}
	injectResponse(t, em, session.ID, 0, OpcodeSubscribeResponse, subResp)

	r := <-ch
	if r.err != nil {
		t.Fatalf("Subscribe: %v", r.err)
	}
	if r.sub == nil {
		t.Fatal("subscription should not be nil")
	}
	if r.sub.ID != 42 {
		t.Errorf("SubscriptionID = %d, want 42", r.sub.ID)
	}
	if r.sub.MaxInterval != 30 {
		t.Errorf("MaxInterval = %d, want 30", r.sub.MaxInterval)
	}

	// The priming report must be delivered as the first Reports batch.
	select {
	case reports := <-r.sub.Reports:
		if len(reports) != 1 || reports[0].Data == nil {
			t.Fatalf("priming report = %+v, want one Data-bearing entry", reports)
		}
		if reports[0].Data.DataVersion != 1 {
			t.Errorf("priming DataVersion = %d, want 1", reports[0].Data.DataVersion)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for priming report")
	}

	// 3. Send a periodic report.
	time.Sleep(50 * time.Millisecond)
	periodicReport := ReportData{
		AttributeReports: []AttributeReport{
			{
				Data: &AttributeData{
					DataVersion: 2,
					Path:        NewAttributePath(1, 0x0006, 0x0000),
					Data:        []byte{0x09}, // TLV true
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeReportData, periodicReport)

	select {
	case reports := <-r.sub.Reports:
		if len(reports) != 1 {
			t.Fatalf("periodic reports len = %d, want 1", len(reports))
		}
		if reports[0].Data == nil {
			t.Fatal("report Data should not be nil")
		}
		if reports[0].Data.DataVersion != 2 {
			t.Errorf("DataVersion = %d, want 2", reports[0].Data.DataVersion)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for periodic report")
	}

	r.sub.Cancel()
}

func TestClient_Subscribe_Error(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 10, Type: protocol.SessionCASE}
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

	time.Sleep(50 * time.Millisecond)

	// Respond with an error status instead of priming report.
	statusResp := StatusResponseMessage{
		Status: uint8(StatusResourceExhausted),
	}
	injectResponse(t, em, session.ID, 0, OpcodeStatusResponse, statusResp)

	r := <-ch
	if r.err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsStatus(r.err, StatusResourceExhausted) {
		t.Errorf("expected StatusResourceExhausted, got: %v", r.err)
	}
}

// TestClient_Subscribe_PrimingAttributeStatusIsError verifies that a
// priming ReportData carrying a per-attribute status failure (rather than
// data) causes Subscribe to return an error — establishment must not be
// reported as successful when the priming value itself failed.
func TestClient_Subscribe_PrimingAttributeStatusIsError(t *testing.T) {
	em := protocol.NewExchangeManager()
	session := &protocol.Session{ID: 11, Type: protocol.SessionCASE}
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

	time.Sleep(50 * time.Millisecond)

	primingReport := ReportData{
		AttributeReports: []AttributeReport{
			{
				Status: &AttributeStatus{
					Path:   NewAttributePath(1, 0x0006, 0x0000),
					Status: StatusIB{Status: uint8(StatusUnsupportedAttribute)},
				},
			},
		},
	}
	injectResponse(t, em, session.ID, 0, OpcodeReportData, primingReport)

	r := <-ch
	if r.sub != nil {
		t.Fatal("expected no subscription when the priming report fails")
	}
	if !IsStatus(r.err, StatusUnsupportedAttribute) {
		t.Errorf("expected StatusUnsupportedAttribute, got: %v", r.err)
	}
}

func TestClient_NewClient(t *testing.T) {
	em := protocol.NewExchangeManager()
	client := NewClient(em)
	if client == nil {
		t.Fatal("NewClient should not return nil")
	}
	if client.exchangeManager != em {
		t.Error("exchangeManager mismatch")
	}
}

func TestCheckStatusResponse(t *testing.T) {
	t.Run("non-status message passes through", func(t *testing.T) {
		msg := &protocol.Message{
			Protocol: protocol.ProtocolHeader{
				ProtocolOpcode: OpcodeReportData,
			},
		}
		err := checkStatusResponse(msg)
		if err != nil {
			t.Errorf("expected nil error for non-status message, got: %v", err)
		}
	})

	t.Run("success status returns nil", func(t *testing.T) {
		statusResp := StatusResponseMessage{
			Status: uint8(StatusSuccess),
		}
		data, _ := tlv.Marshal(statusResp)
		msg := &protocol.Message{
			Protocol: protocol.ProtocolHeader{
				ProtocolOpcode: OpcodeStatusResponse,
			},
			Payload: data,
		}
		err := checkStatusResponse(msg)
		if err != nil {
			t.Errorf("expected nil error for success status, got: %v", err)
		}
	})

	t.Run("error status returns StatusError", func(t *testing.T) {
		statusResp := StatusResponseMessage{
			Status: uint8(StatusTimeout),
		}
		data, _ := tlv.Marshal(statusResp)
		msg := &protocol.Message{
			Protocol: protocol.ProtocolHeader{
				ProtocolOpcode: OpcodeStatusResponse,
			},
			Payload: data,
		}
		err := checkStatusResponse(msg)
		if err == nil {
			t.Fatal("expected error for timeout status")
		}
		if !IsStatus(err, StatusTimeout) {
			t.Errorf("expected StatusTimeout, got: %v", err)
		}
	})
}

func TestBuildIMMessage(t *testing.T) {
	payload := StatusIB{Status: uint8(StatusSuccess)}
	msg := buildIMMessage(5, 10, OpcodeStatusResponse, payload)

	if msg.Header.SessionID != 5 {
		t.Errorf("SessionID = %d, want 5", msg.Header.SessionID)
	}
	if msg.Protocol.ExchangeID != 10 {
		t.Errorf("ExchangeID = %d, want 10", msg.Protocol.ExchangeID)
	}
	if msg.Protocol.ProtocolOpcode != OpcodeStatusResponse {
		t.Errorf("ProtocolOpcode = 0x%02X, want 0x%02X", msg.Protocol.ProtocolOpcode, OpcodeStatusResponse)
	}
	if msg.Protocol.ProtocolID != ProtocolID {
		t.Errorf("ProtocolID = 0x%04X, want 0x%04X", msg.Protocol.ProtocolID, ProtocolID)
	}
	if len(msg.Payload) == 0 {
		t.Error("Payload should not be empty")
	}
}
