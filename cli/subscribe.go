// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/tlv"

	"github.com/spf13/cobra"
)

// subscribeOptions holds the fully resolved, validated parameters for a
// single foreground attribute subscription.
type subscribeOptions struct {
	nodeID   uint64
	endpoint uint16
	cl       *clusters.ClusterInfo
	attr     *clusters.AttributeInfo
	min      uint16
	max      uint16
	count    uint32        // 0 = unlimited
	duration time.Duration // 0 = unlimited
}

// addSubscribeIntervalFlags registers the subscription-specific flags shared
// by the generic `cluster subscribe` command and the per-cluster shorthand
// `subscribe <attribute>` command.
func addSubscribeIntervalFlags(cmd *cobra.Command) {
	cmd.Flags().Uint16P("min", "m", 1, "minimum reporting interval in seconds")
	cmd.Flags().Uint16P("max", "M", 60, "maximum reporting interval in seconds")
	cmd.Flags().Uint32P("count", "n", 0, "stop after this many emitted records (0 = unlimited)")
	cmd.Flags().DurationP("duration", "d", 0, "stop this long after establishment (0 = unlimited)")
}

// validateSubscribeIntervals enforces the Matter SubscribeRequest interval
// contract before any network activity: the minimum may be zero, the
// maximum must be at least one second, and the maximum must be greater than
// or equal to the minimum (equal bounds request a fixed cadence).
func validateSubscribeIntervals(min, max uint16) error {
	if max < 1 {
		return fmt.Errorf("--max must be at least 1 second")
	}
	if max < min {
		return fmt.Errorf("--max (%ds) must be >= --min (%ds)", max, min)
	}
	return nil
}

// subscribeOptionsFromFlags reads and validates the subscription flags added
// by addSubscribeIntervalFlags, returning an error before any connection is
// attempted if the intervals are invalid.
func subscribeOptionsFromFlags(cmd *cobra.Command, nodeID uint64, endpoint uint16, cl *clusters.ClusterInfo, attr *clusters.AttributeInfo) (subscribeOptions, error) {
	minInt, _ := cmd.Flags().GetUint16("min")
	maxInt, _ := cmd.Flags().GetUint16("max")
	count, _ := cmd.Flags().GetUint32("count")
	duration, _ := cmd.Flags().GetDuration("duration")

	if err := validateSubscribeIntervals(minInt, maxInt); err != nil {
		return subscribeOptions{}, err
	}

	return subscribeOptions{
		nodeID:   nodeID,
		endpoint: endpoint,
		cl:       cl,
		attr:     attr,
		min:      minInt,
		max:      maxInt,
		count:    count,
		duration: duration,
	}, nil
}

// subscribeBoundReached reports whether a count bound has been reached. It
// is factored out as a pure function so the count/duration precedence rule
// ("stop successfully when either limit is reached first") is unit-testable
// without a live subscription.
func subscribeBoundReached(count, emitted uint32) bool {
	return count > 0 && emitted >= count
}

// newSubscribeLogger returns a function that writes routine lifecycle
// progress to cmd's stderr. Output is suppressed unless verbose logging is
// enabled or stderr is a terminal, so piped stdout stays parseable and
// unattended stderr stays quiet. It never animates — subscribe streams data
// continuously, so a spinner would fight with the record output.
func newSubscribeLogger(cmd *cobra.Command, verbose bool) func(string) {
	w := cmd.ErrOrStderr()
	quiet := !verbose && !output.IsStderrTTY()
	return func(msg string) {
		if verbose {
			slog.Info(msg)
			return
		}
		if quiet {
			return
		}
		fmt.Fprintln(w, msg)
	}
}

// subscribeWarn prints a warning to cmd's stderr. Unlike routine lifecycle
// progress, warnings are never suppressed.
func subscribeWarn(cmd *cobra.Command, verbose bool, msg string) {
	if verbose {
		slog.Warn(msg)
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", output.WarningIcon(), msg)
}

// runSubscribe establishes a foreground subscription to a single attribute
// and streams reports until it is cancelled (Ctrl+C), a requested count or
// duration bound is reached, or an error occurs. Attribute records are
// written exclusively to stdout in the format requested by --format;
// lifecycle progress, warnings, and errors go to stderr. Subscriptions
// bypass the session daemon entirely, even when --keep-alive is set — see
// connectToNode.
func runSubscribe(cmd *cobra.Command, opts subscribeOptions) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	format, _ := cmd.Flags().GetString("format")
	logStep := newSubscribeLogger(cmd, verbose)

	// Subscriptions never use the session daemon (they own their CASE
	// session in the foreground for the life of the command), and
	// maybeStartDaemon skips spawning one for this command entirely — see
	// bypassDaemonAnnotation in root.go. Tell the user explicitly rather
	// than silently ignoring their flag.
	if ka, _ := cmd.Flags().GetString("keep-alive"); ka != "" {
		subscribeWarn(cmd, verbose, "--keep-alive is ignored for subscribe; subscriptions always run in the foreground")
	}

	logStep(fmt.Sprintf("Subscribing to %s/%s on %s endpoint %s %s",
		output.Bold(opts.cl.DisplayName), output.Info(opts.attr.DisplayName),
		output.Bold(resolveNodeLabel(opts.nodeID)), output.Bold(fmt.Sprintf("%d", opts.endpoint)),
		output.Muted(fmt.Sprintf("(0x%04X/0x%04X) [%d..%d]s", opts.cl.ID, opts.attr.ID, opts.min, opts.max))))

	// Intercept SIGINT so Ctrl+C cancels the subscription cleanly and exits 0.
	sigCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	client, session, cleanup, err := connectToNode(sigCtx, opts.nodeID)
	if err != nil {
		return err
	}
	defer cleanup()

	path := interaction.NewAttributePath(opts.endpoint, opts.cl.ID, opts.attr.ID)
	sub, err := client.Subscribe(sigCtx, session, []interaction.AttributePath{path}, opts.min, opts.max)
	if err != nil {
		return fmt.Errorf("subscribing to attribute: %w", err)
	}
	defer sub.Cancel()

	logStep(fmt.Sprintf("Subscribed (ID %d, negotiated max interval %ds) — streaming to stdout, Ctrl+C to stop",
		sub.ID, sub.MaxInterval))

	enc := output.NewSubscribeEncoder(format, cmd.OutOrStdout())

	// The duration bound excludes connect/CASE handshake time and starts
	// only once the subscription is established.
	var durationCh <-chan time.Time
	if opts.duration > 0 {
		timer := time.NewTimer(opts.duration)
		defer timer.Stop()
		durationCh = timer.C
	}

	var emitted uint32
	for {
		select {
		case reports, ok := <-sub.Reports:
			if !ok {
				return nil
			}
			for _, r := range reports {
				if r.Data == nil {
					continue
				}
				rec := buildSubscribeRecord(opts, r.Data)
				if err := enc.Encode(rec); err != nil {
					return fmt.Errorf("writing subscription record: %w", err)
				}
				emitted++
				if subscribeBoundReached(opts.count, emitted) {
					return nil
				}
			}

		case err, ok := <-sub.Errors:
			if !ok {
				return nil
			}
			return fmt.Errorf("subscription error: %w", err)

		case <-sigCtx.Done():
			return nil

		case <-durationCh:
			if emitted == 0 {
				subscribeWarn(cmd, verbose, fmt.Sprintf(
					"subscription established successfully; no attribute reports were emitted during the %s window", opts.duration))
			}
			return nil
		}
	}
}

// buildSubscribeRecord converts a decoded AttributeData report into an
// output.SubscribeRecord. On a TLV decode failure, Value is left nil and
// Raw/DecodeError describe the failure instead, so one unfamiliar value
// doesn't terminate an otherwise healthy stream.
func buildSubscribeRecord(opts subscribeOptions, data *interaction.AttributeData) output.SubscribeRecord {
	dv := data.DataVersion
	rec := output.SubscribeRecord{
		Timestamp:   time.Now(),
		NodeID:      opts.nodeID,
		Endpoint:    opts.endpoint,
		ClusterID:   opts.cl.ID,
		Cluster:     opts.cl.DisplayName,
		AttributeID: opts.attr.ID,
		Attribute:   opts.attr.DisplayName,
		DataVersion: &dv,
		Display:     formatAttrValue(data.Data, opts.attr.Type),
	}

	value, err := decodeTLVNative(data.Data)
	if err != nil {
		rec.Raw = fmt.Sprintf("0x%s", hex.EncodeToString(data.Data))
		rec.DecodeError = err.Error()
		return rec
	}
	rec.Value = value
	return rec
}

// decodeTLVNative decodes raw TLV bytes into a native Go value (bool,
// uint64, int64, float32/float64, string, nil, []any, or map[string]any),
// preserving scalar and aggregate types for JSON/YAML consumers instead of
// routing them through a human display string. Octet strings are rendered
// as a "0x"-prefixed hex string, consistent with how the CLI already
// accepts octet values on write. Struct fields are keyed by their decimal
// TLV context tag number, since this generic decoder has no field-name
// metadata to draw on (unlike the cluster/attribute registry).
func decodeTLVNative(raw []byte) (any, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty TLV payload")
	}
	r := tlv.NewReader(bytes.NewReader(raw))
	if err := r.Next(); err != nil {
		return nil, fmt.Errorf("reading TLV element: %w", err)
	}
	return decodeTLVElementNative(r)
}

// decodeTLVElementNative decodes the TLV element the reader is currently
// positioned on (after Next() has been called).
func decodeTLVElementNative(r *tlv.Reader) (any, error) {
	switch r.Type() {
	case tlv.TypeArray, tlv.TypeList:
		return decodeTLVArrayNative(r)
	case tlv.TypeStructure:
		return decodeTLVStructNative(r)
	}

	switch val := r.Value().(type) {
	case []byte:
		return fmt.Sprintf("0x%s", hex.EncodeToString(val)), nil
	default:
		return val, nil
	}
}

// decodeTLVArrayNative decodes an array or list container into a []any,
// using the same tlvChildren walker formatTLVContainer uses for display
// (cli/cluster.go) — unlike that formatter, a read or decode failure here is
// fatal rather than best-effort, since a native-typed record must not
// silently drop data a consumer will parse programmatically.
func decodeTLVArrayNative(r *tlv.Reader) ([]any, error) {
	// Non-nil from the start: an empty list attribute (an unpopulated
	// AcceptedCommandList, say) must marshal as [] and not null, or a
	// consumer iterating the value has to special-case it.
	out := []any{}
	err := tlvChildren(r, func(r *tlv.Reader) error {
		elem, err := decodeTLVElementNative(r)
		if err != nil {
			return err
		}
		out = append(out, elem)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading array element: %w", err)
	}
	return out, nil
}

// decodeTLVStructNative decodes a structure container into a map[string]any
// keyed by decimal TLV context tag number, using the same tlvChildren walker
// formatTLVContainer uses for display (cli/cluster.go).
func decodeTLVStructNative(r *tlv.Reader) (map[string]any, error) {
	out := make(map[string]any)
	err := tlvChildren(r, func(r *tlv.Reader) error {
		tag := r.TagValue()
		elem, err := decodeTLVElementNative(r)
		if err != nil {
			return err
		}
		out[strconv.FormatUint(uint64(tag.TagNum), 10)] = elem
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading struct field: %w", err)
	}
	return out, nil
}
