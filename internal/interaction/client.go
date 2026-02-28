// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/tlv"
)

// Client provides high-level Interaction Model operations. It uses an
// ExchangeManager to create exchanges and communicate with Matter peers.
type Client struct {
	exchangeManager *protocol.ExchangeManager
}

// NewClient creates a new IM client backed by the given ExchangeManager.
func NewClient(em *protocol.ExchangeManager) *Client {
	return &Client{exchangeManager: em}
}

// Read sends a ReadRequest for the given attribute paths and collects the full
// ReportData response(s). It handles chunked responses (MoreChunkedMessages)
// automatically by sending StatusResponse(Success) acknowledgments.
func (c *Client) Read(ctx context.Context, session *protocol.Session, paths ...AttributePath) ([]AttributeReport, error) {
	if len(paths) == 1 {
		slog.Debug("interaction: read", "path", paths[0])
	} else {
		slog.Debug("interaction: read", "paths", len(paths))
	}

	exchange, err := c.exchangeManager.NewExchange(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("interaction: creating exchange: %w", err)
	}
	defer c.exchangeManager.CloseExchange(exchange)

	req := ReadRequest{
		AttributeRequests: paths,
		FabricFiltered:    true,
	}

	if err := sendIMMessage(ctx, exchange, OpcodeReadRequest, req); err != nil {
		return nil, fmt.Errorf("interaction: sending read request: %w", err)
	}

	var allReports []AttributeReport

	for {
		msg, err := exchange.Receive(ctx)
		if err != nil {
			return nil, fmt.Errorf("interaction: receiving response: %w", err)
		}

		if err := checkStatusResponse(msg); err != nil {
			return nil, err
		}

		if msg.Protocol.ProtocolOpcode != OpcodeReportData {
			return nil, fmt.Errorf("interaction: unexpected opcode 0x%02X, want ReportData (0x%02X)",
				msg.Protocol.ProtocolOpcode, OpcodeReportData)
		}

		var report ReportData
		if err := tlv.Unmarshal(msg.Payload, &report); err != nil {
			return nil, fmt.Errorf("interaction: decoding report data: %w", err)
		}

		allReports = append(allReports, report.AttributeReports...)

		if report.MoreChunkedMessages == nil || !*report.MoreChunkedMessages {
			slog.Debug("interaction: read complete", "reports", len(allReports))
			break
		}
		slog.Debug("interaction: read chunked, requesting next chunk", "so_far", len(allReports))

		// Send StatusResponse(Success) to request the next chunk.
		ack := StatusResponseMessage{
			Status: uint8(StatusSuccess),
		}
		if err := sendIMMessage(ctx, exchange, OpcodeStatusResponse, ack); err != nil {
			return nil, fmt.Errorf("interaction: sending chunk ack: %w", err)
		}
	}

	return allReports, nil
}

// Write sends a WriteRequest with the given attribute writes and returns the
// per-attribute write status results.
func (c *Client) Write(ctx context.Context, session *protocol.Session, writes ...AttributeWrite) ([]AttributeStatus, error) {
	if len(writes) == 1 {
		slog.Debug("interaction: write", "path", writes[0].Path)
	} else {
		slog.Debug("interaction: write", "count", len(writes))
	}

	exchange, err := c.exchangeManager.NewExchange(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("interaction: creating exchange: %w", err)
	}
	defer c.exchangeManager.CloseExchange(exchange)

	req := WriteRequest{
		WriteRequests: writes,
	}

	if err := sendIMMessage(ctx, exchange, OpcodeWriteRequest, req); err != nil {
		return nil, fmt.Errorf("interaction: sending write request: %w", err)
	}

	msg, err := exchange.Receive(ctx)
	if err != nil {
		return nil, fmt.Errorf("interaction: receiving response: %w", err)
	}

	if err := checkStatusResponse(msg); err != nil {
		return nil, err
	}

	if msg.Protocol.ProtocolOpcode != OpcodeWriteResponse {
		return nil, fmt.Errorf("interaction: unexpected opcode 0x%02X, want WriteResponse (0x%02X)",
			msg.Protocol.ProtocolOpcode, OpcodeWriteResponse)
	}

	var resp WriteResponse
	if err := tlv.Unmarshal(msg.Payload, &resp); err != nil {
		return nil, fmt.Errorf("interaction: decoding write response: %w", err)
	}

	slog.Debug("interaction: write complete", "statuses", len(resp.WriteResponses))
	return resp.WriteResponses, nil
}

// Invoke sends an InvokeRequest with a single command and returns the response.
// The fields parameter contains the raw TLV-encoded command fields, or nil
// if the command takes no arguments.
func (c *Client) Invoke(ctx context.Context, session *protocol.Session, path CommandPath, fields []byte) (*InvokeResponseIB, error) {
	return c.invokeInternal(ctx, session, path, fields, 0)
}

// InvokeTimed sends a Timed Invoke: first sends a TimedRequest with the given
// timeout (in milliseconds), waits for the StatusResponse(Success), then sends
// the InvokeRequest with TimedRequest=true.
func (c *Client) InvokeTimed(ctx context.Context, session *protocol.Session, path CommandPath, fields []byte, timeoutMs uint16) (*InvokeResponseIB, error) {
	return c.invokeInternal(ctx, session, path, fields, timeoutMs)
}

// TimedRequestMessage is the TLV payload of a TimedRequest (opcode 0x0A).
type TimedRequestMessage struct {
	Timeout uint16 `tlv:"0,uint"`
}

// invokeResponseTimeout is the default maximum time to wait for an
// InvokeResponse after the InvokeRequest has been sent. This prevents the
// caller from hanging indefinitely when the device disconnects or crashes
// mid-command (e.g. during AddNOC processing) and the transport layer has
// not yet detected the disconnection.
//
// 30 seconds is generous enough for even the slowest embedded devices
// (certificate installation, key derivation, flash writes) while still
// providing a reasonable upper bound for user-facing commands.
//
// Callers that need a longer timeout (e.g. ConnectNetwork for Thread
// devices, which must join the mesh before responding) can override this
// per-call via WithInvokeTimeout.
const invokeResponseTimeout = 30 * time.Second

// invokeTimeoutKey is the context key for overriding invokeResponseTimeout.
type invokeTimeoutKey struct{}

// WithInvokeTimeout returns a child context that overrides the default invoke
// response timeout used by Client.Invoke / Client.InvokeTimed. This is useful
// for commands whose response is expected to take longer than the default 30 s
// (e.g. NetworkCommissioning.ConnectNetwork on Thread devices).
func WithInvokeTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, invokeTimeoutKey{}, d)
}

// getInvokeTimeout returns the invoke response timeout to use: either the
// per-call override stored in ctx, or the package-level default.
func getInvokeTimeout(ctx context.Context) time.Duration {
	if d, ok := ctx.Value(invokeTimeoutKey{}).(time.Duration); ok && d > 0 {
		return d
	}
	return invokeResponseTimeout
}

func (c *Client) invokeInternal(ctx context.Context, session *protocol.Session, path CommandPath, fields []byte, timedMs uint16) (*InvokeResponseIB, error) {
	if timedMs > 0 {
		slog.Debug("interaction: invoke (timed)", "path", path, "timeoutMs", timedMs)
	} else {
		slog.Debug("interaction: invoke", "path", path)
	}

	exchange, err := c.exchangeManager.NewExchange(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("interaction: creating exchange: %w", err)
	}
	defer c.exchangeManager.CloseExchange(exchange)

	// For timed invoke: send TimedRequest, wait for StatusResponse, then
	// send InvokeRequest with the ACK piggybacked.
	var ackCounter uint32
	var hasAck bool
	if timedMs > 0 {
		timedReq := TimedRequestMessage{Timeout: timedMs}
		if err := sendIMMessage(ctx, exchange, OpcodeTimedRequest, timedReq); err != nil {
			return nil, fmt.Errorf("interaction: sending timed request: %w", err)
		}

		// Wait for StatusResponse(Success).
		timedResp, err := exchange.Receive(ctx)
		if err != nil {
			return nil, fmt.Errorf("interaction: receiving timed response: %w", err)
		}
		if err := checkStatusResponse(timedResp); err != nil {
			return nil, fmt.Errorf("interaction: timed request rejected: %w", err)
		}
		// Save counter to piggyback ACK on the InvokeRequest.
		ackCounter = timedResp.Header.MessageCounter
		hasAck = true
	}

	if fields == nil {
		fields = []byte{}
	}

	req := InvokeRequest{
		SuppressResponse: false,
		TimedRequest:     timedMs > 0,
		InvokeRequests: []CommandDataIB{
			{Path: path, Fields: fields},
		},
	}

	if err := sendIMMessageWithACK(ctx, exchange, OpcodeInvokeRequest, req, hasAck, ackCounter); err != nil {
		return nil, fmt.Errorf("interaction: sending invoke request: %w", err)
	}

	// Apply a response timeout so we never block indefinitely waiting for
	// a device that has disconnected or crashed. If the caller's context
	// already has a tighter deadline, that takes precedence. Callers may
	// override the default via WithInvokeTimeout for long-running commands
	// (e.g. ConnectNetwork on Thread devices).
	recvCtx, recvCancel := context.WithTimeout(ctx, getInvokeTimeout(ctx))
	defer recvCancel()

	msg, err := exchange.Receive(recvCtx)
	if err != nil {
		return nil, fmt.Errorf("interaction: receiving response: %w", err)
	}

	if err := checkStatusResponse(msg); err != nil {
		return nil, err
	}

	if msg.Protocol.ProtocolOpcode != OpcodeInvokeResponse {
		return nil, fmt.Errorf("interaction: unexpected opcode 0x%02X, want InvokeResponse (0x%02X)",
			msg.Protocol.ProtocolOpcode, OpcodeInvokeResponse)
	}

	var resp InvokeResponse
	if err := tlv.Unmarshal(msg.Payload, &resp); err != nil {
		return nil, fmt.Errorf("interaction: decoding invoke response: %w", err)
	}

	if len(resp.InvokeResponses) == 0 {
		return nil, fmt.Errorf("interaction: invoke response contains no results")
	}

	slog.Debug("interaction: invoke complete", "path", path)
	return &resp.InvokeResponses[0], nil
}

// Subscribe sends a SubscribeRequest and returns a Subscription that delivers
// periodic attribute reports. The subscription runs in the background and
// delivers reports through the Subscription.Reports channel.
func (c *Client) Subscribe(ctx context.Context, session *protocol.Session, paths []AttributePath, minInterval, maxInterval uint16) (*Subscription, error) {
	exchange, err := c.exchangeManager.NewExchange(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("interaction: creating exchange: %w", err)
	}

	req := SubscribeRequest{
		KeepSubscriptions:  true,
		MinIntervalFloor:   minInterval,
		MaxIntervalCeiling: maxInterval,
		AttributeRequests:  paths,
		FabricFiltered:     true,
	}

	if err := sendIMMessage(ctx, exchange, OpcodeSubscribeRequest, req); err != nil {
		c.exchangeManager.CloseExchange(exchange)
		return nil, fmt.Errorf("interaction: sending subscribe request: %w", err)
	}

	// Wait for the initial ReportData (priming report).
	msg, err := exchange.Receive(ctx)
	if err != nil {
		c.exchangeManager.CloseExchange(exchange)
		return nil, fmt.Errorf("interaction: receiving priming report: %w", err)
	}

	if err := checkStatusResponse(msg); err != nil {
		c.exchangeManager.CloseExchange(exchange)
		return nil, err
	}

	if msg.Protocol.ProtocolOpcode != OpcodeReportData {
		c.exchangeManager.CloseExchange(exchange)
		return nil, fmt.Errorf("interaction: unexpected opcode 0x%02X, want ReportData (0x%02X)",
			msg.Protocol.ProtocolOpcode, OpcodeReportData)
	}

	// Send StatusResponse(Success) to acknowledge the priming report.
	ack := StatusResponseMessage{
		Status: uint8(StatusSuccess),
	}
	if err := sendIMMessage(ctx, exchange, OpcodeStatusResponse, ack); err != nil {
		c.exchangeManager.CloseExchange(exchange)
		return nil, fmt.Errorf("interaction: acknowledging priming report: %w", err)
	}

	// Wait for the SubscribeResponse.
	msg, err = exchange.Receive(ctx)
	if err != nil {
		c.exchangeManager.CloseExchange(exchange)
		return nil, fmt.Errorf("interaction: receiving subscribe response: %w", err)
	}

	if msg.Protocol.ProtocolOpcode != OpcodeSubscribeResponse {
		c.exchangeManager.CloseExchange(exchange)
		return nil, fmt.Errorf("interaction: unexpected opcode 0x%02X, want SubscribeResponse (0x%02X)",
			msg.Protocol.ProtocolOpcode, OpcodeSubscribeResponse)
	}

	var subResp SubscribeResponse
	if err := tlv.Unmarshal(msg.Payload, &subResp); err != nil {
		c.exchangeManager.CloseExchange(exchange)
		return nil, fmt.Errorf("interaction: decoding subscribe response: %w", err)
	}

	reports := make(chan []AttributeReport, 16)
	errs := make(chan error, 1)

	subCtx, cancel := context.WithCancel(ctx)

	sub := &Subscription{
		ID:      subResp.SubscriptionID,
		Reports: reports,
		Errors:  errs,
		cancel:  cancel,
	}

	// Run background goroutine to receive periodic reports.
	go func() {
		defer c.exchangeManager.CloseExchange(exchange)
		defer close(reports)
		defer close(errs)

		for {
			msg, err := exchange.Receive(subCtx)
			if err != nil {
				if subCtx.Err() != nil {
					return // cancelled
				}
				select {
				case errs <- fmt.Errorf("interaction: receiving subscription report: %w", err):
				default:
				}
				return
			}

			if msg.Protocol.ProtocolOpcode == OpcodeStatusResponse {
				var statusMsg StatusResponseMessage
				if unmarshalErr := tlv.Unmarshal(msg.Payload, &statusMsg); unmarshalErr == nil {
					if sErr := statusFromCode(statusMsg.Status); sErr != nil {
						select {
						case errs <- sErr:
						default:
						}
						return
					}
				}
				continue
			}

			if msg.Protocol.ProtocolOpcode != OpcodeReportData {
				continue
			}

			var report ReportData
			if err := tlv.Unmarshal(msg.Payload, &report); err != nil {
				select {
				case errs <- fmt.Errorf("interaction: decoding subscription report: %w", err):
				default:
				}
				return
			}

			if len(report.AttributeReports) > 0 {
				select {
				case reports <- report.AttributeReports:
				case <-subCtx.Done():
					return
				}
			}

			// Acknowledge the report.
			reportAck := StatusResponseMessage{
				Status: uint8(StatusSuccess),
			}
			if err := sendIMMessage(subCtx, exchange, OpcodeStatusResponse, reportAck); err != nil {
				select {
				case errs <- fmt.Errorf("interaction: acknowledging subscription report: %w", err):
				default:
				}
				return
			}
		}
	}()

	return sub, nil
}

// sendIMMessage marshals a TLV struct and sends it via the exchange's Send method.
// It injects the InteractionModelRevision field (tag 0xFF = 11) required by
// Matter 1.1+ before the final EndOfContainer marker.
func sendIMMessage(ctx context.Context, exchange *protocol.Exchange, opcode byte, payload any) error {
	return sendIMMessageWithACK(ctx, exchange, opcode, payload, false, 0)
}

// sendIMMessageWithACK is like sendIMMessage but optionally piggybacks an MRP ACK.
func sendIMMessageWithACK(ctx context.Context, exchange *protocol.Exchange, opcode byte, payload any, ack bool, ackCounter uint32) error {
	data, err := tlv.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling IM payload: %w", err)
	}

	// All IM messages must include InteractionModelRevision at tag 0xFF.
	// Insert it before the final EndOfContainer byte of the outer struct.
	if len(data) > 0 && data[len(data)-1] == byte(tlv.TypeEndOfContainer) {
		// ContextTag(0xFF) + UnsignedInt8 = control byte 0x24, tag 0xFF, value 11
		imRevision := []byte{0x24, 0xFF, 0x0B}
		data = append(data[:len(data)-1], imRevision...)
		data = append(data, byte(tlv.TypeEndOfContainer))
	}

	slog.Debug("interaction: sendIMMessage", "opcode", fmt.Sprintf("0x%02x", opcode), "payloadLen", len(data), "payloadHex", hex.EncodeToString(data))

	flags := protocol.ExFlagReliable
	msg := &protocol.Message{
		Protocol: protocol.ProtocolHeader{
			ProtocolOpcode: opcode,
			ProtocolID:     ProtocolID,
			ExchangeFlags:  flags,
		},
		Payload: data,
	}

	// Piggyback an MRP ACK if requested.
	if ack {
		msg.Protocol.ExchangeFlags |= protocol.ExFlagACK
		msg.Protocol.HasAckCounter = true
		msg.Protocol.AckMessageCounter = ackCounter
	}

	return exchange.Send(ctx, msg)
}

// checkStatusResponse checks whether a message is a StatusResponse with a
// non-success status. If so, it returns a StatusError. If the message is
// not a StatusResponse, it returns nil (the caller should continue processing).
func checkStatusResponse(msg *protocol.Message) error {
	if msg.Protocol.ProtocolOpcode != OpcodeStatusResponse {
		return nil
	}

	var statusMsg StatusResponseMessage
	if err := tlv.Unmarshal(msg.Payload, &statusMsg); err != nil {
		return fmt.Errorf("interaction: decoding status response: %w", err)
	}

	return statusFromCode(statusMsg.Status)
}
