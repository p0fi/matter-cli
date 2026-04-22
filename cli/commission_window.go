// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/clusters/administratorcommissioning"
	"github.com/p0fi/matter-cli/internal/commissioning"
	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/tlv"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// timedInteractionTimeoutMs is the spec-mandated window for completing a Timed
// Invoke. The client sends TimedRequest with this timeout, and the server
// rejects the follow-up InvokeRequest if it arrives after it expires. It is
// unrelated to the commissioning window duration.
const timedInteractionTimeoutMs uint16 = 5_000

// Administrator Commissioning cluster-specific status codes (Matter spec §11.19.6).
const (
	adminBusy              uint8 = 0x02
	adminPAKEParamError    uint8 = 0x03
	adminWindowNotOpen     uint8 = 0x04
)

// newCommissionOpenWindowCmd creates the `commission open-window` subcommand.
func newCommissionOpenWindowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open-window",
		Short: "Open an Enhanced Commissioning Method window on a commissioned device",
		Long: `Ask an already-commissioned device to open a fresh commissioning window so a
second ecosystem (Apple Home, Google Home, Alexa, etc.) can commission it
without a factory reset.

By default a unique passcode, salt, and discriminator are generated, the PAKE
verifier is derived locally, and the OpenCommissioningWindow command is sent
over a Timed Invoke. The resulting QR code and manual pairing code are printed
for use by the second ecosystem.`,
		Example: `  matter @1 commission open-window
  matter @1 commission open-window --timeout 5m
  matter @1 commission open-window --passcode 20202021 --discriminator 3840
  matter @1 commission open-window --basic      # Basic Commissioning Method`,
		// Override the parent's daemon-guard: opening a window only invokes a
		// command, it does not need exclusive DB access. Fall through to the
		// root PersistentPreRunE for logging and target resolution.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Root().PersistentPreRunE(cmd, args)
		},
		RunE: runOpenWindow,
	}
	cmd.Flags().Duration("timeout", 3*time.Minute, "how long the window stays open (180s-900s)")
	cmd.Flags().Uint32("iterations", uint32(commissioning.MinIterations), "PBKDF2 iteration count (1000-100000)")
	cmd.Flags().Uint32("passcode", 0, "explicit 27-bit passcode (default: random)")
	cmd.Flags().Uint16("discriminator", 0, "explicit 12-bit discriminator (default: random)")
	cmd.Flags().String("salt-hex", "", "explicit hex-encoded PAKE salt, 16-32 bytes (default: random 16 bytes)")
	cmd.Flags().Bool("basic", false, "use Basic Commissioning Method instead of ECM (passes no verifier)")
	return cmd
}

// newCommissionCloseWindowCmd creates the `commission close-window` subcommand.
func newCommissionCloseWindowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "close-window",
		Aliases: []string{"revoke-window"},
		Short:   "Revoke an open commissioning window on a commissioned device",
		Example: `  matter @1 commission close-window`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Root().PersistentPreRunE(cmd, args)
		},
		RunE: runCloseWindow,
	}
	return cmd
}

// runOpenWindow implements `commission open-window`.
func runOpenWindow(cmd *cobra.Command, _ []string) error {
	nodeID, _, err := requireTarget(cmd)
	if err != nil {
		return err
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")
	// Bounds-check in int64 seconds before narrowing to uint16 so that values
	// larger than 65535 s cannot wrap into the valid [180,900] range.
	timeoutSecsI64 := int64(timeout / time.Second)
	if timeoutSecsI64 < int64(commissioning.MinCommissioningTimeoutSeconds) ||
		timeoutSecsI64 > int64(commissioning.MaxCommissioningTimeoutSeconds) {
		return fmt.Errorf("--timeout must be between %ds and %ds",
			commissioning.MinCommissioningTimeoutSeconds,
			commissioning.MaxCommissioningTimeoutSeconds)
	}
	timeoutSecs := uint16(timeoutSecsI64)

	basic, _ := cmd.Flags().GetBool("basic")

	verbose, _ := cmd.Flags().GetBool("verbose")
	stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

	stepper.Step(fmt.Sprintf("Opening commissioning window on %s (timeout %s)",
		output.Bold(resolveNodeLabel(nodeID)), output.Accent(timeout.String())))

	// Resolve passcode, salt, discriminator (only needed for ECM).
	passcode, salt, disc, err := resolveWindowParams(cmd, basic)
	if err != nil {
		stepper.Fail(err.Error())
		return err
	}

	var fields []byte
	var commandID uint32
	if basic {
		commandID = administratorcommissioning.CmdOpenBasicCommissioningWindow
		fields, err = encodeOpenBasicFields(timeoutSecs)
	} else {
		iterations, _ := cmd.Flags().GetUint32("iterations")
		if iterations < uint32(commissioning.MinIterations) || iterations > uint32(commissioning.MaxIterations) {
			err = fmt.Errorf("--iterations must be between %d and %d",
				commissioning.MinIterations, commissioning.MaxIterations)
			stepper.Fail(err.Error())
			return err
		}
		commandID = administratorcommissioning.CmdOpenCommissioningWindow
		verifier, verr := commissioning.ComputePAKEVerifier(passcode, salt, int(iterations))
		if verr != nil {
			stepper.Fail(verr.Error())
			return fmt.Errorf("computing PAKE verifier: %w", verr)
		}
		fields, err = encodeOpenWindowFields(timeoutSecs, verifier, disc, iterations, salt)
	}
	if err != nil {
		stepper.Fail(err.Error())
		return fmt.Errorf("encoding request fields: %w", err)
	}

	ctx := cmd.Context()
	stepper.Step("Sending Timed Invoke")
	if err := invokeAdminCommissioning(ctx, stepper, nodeID, commandID, fields); err != nil {
		return err
	}

	// Load VID/PID for the setup payload. Fall back to zero (still produces a
	// valid QR/manual code, just with zero vendor info).
	vid, pid := loadNodeVIDPID(ctx, nodeID, stepper)

	expiresAt := time.Now().Add(time.Duration(timeoutSecs) * time.Second)

	if basic {
		stepper.Success(fmt.Sprintf("Basic commissioning window open until %s",
			output.Accent(expiresAt.Format(time.RFC3339))))
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s\n",
			output.Label("Expires:"), output.Accent(expiresAt.Format(time.RFC3339)))
		fmt.Fprintf(cmd.OutOrStdout(),
			"  %s  device's original setup code remains valid during this window\n",
			output.Label("Pairing:"))
		return nil
	}

	payload := &commissioning.SetupPayload{
		VendorID:              vid,
		ProductID:             pid,
		Passcode:              passcode,
		Discriminator:         disc,
		CommissioningFlow:     commissioning.FlowStandard,
		DiscoveryCapabilities: commissioning.DiscoveryOnNetwork,
	}
	qr, qrErr := payload.QRCode()
	if qrErr != nil {
		return fmt.Errorf("building QR code: %w", qrErr)
	}
	manual, mErr := payload.ManualPairingCode()
	if mErr != nil {
		return fmt.Errorf("building manual pairing code: %w", mErr)
	}

	stepper.Success(fmt.Sprintf("Commissioning window open until %s",
		output.Accent(expiresAt.Format(time.RFC3339))))

	w := cmd.OutOrStdout()
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s        %s\n", output.Label("QR Code:"), output.Success(qr))
	fmt.Fprintf(w, "  %s    %s\n", output.Label("Manual Code:"), output.Success(manual))
	fmt.Fprintf(w, "  %s       %s\n", output.Label("Passcode:"), output.Accent(fmt.Sprintf("%d", passcode)))
	fmt.Fprintf(w, "  %s  %s\n", output.Label("Discriminator:"), output.Accent(fmt.Sprintf("%d", disc)))
	fmt.Fprintf(w, "  %s           %s\n", output.Label("Salt:"), output.Muted(hex.EncodeToString(salt)))
	fmt.Fprintf(w, "  %s        %s\n", output.Label("Expires:"), output.Accent(expiresAt.Format(time.RFC3339)))
	return nil
}

// runCloseWindow implements `commission close-window`.
func runCloseWindow(cmd *cobra.Command, _ []string) error {
	nodeID, _, err := requireTarget(cmd)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

	stepper.Step(fmt.Sprintf("Revoking commissioning window on %s",
		output.Bold(resolveNodeLabel(nodeID))))

	if err := invokeAdminCommissioning(cmd.Context(), stepper, nodeID,
		administratorcommissioning.CmdRevokeCommissioning, nil); err != nil {
		return err
	}

	stepper.Success("Commissioning window revoked")
	return nil
}

// resolveWindowParams gathers (or generates) the per-window PAKE parameters.
// For Basic Commissioning these values are not used by the device, so we keep
// the logic uniform and just skip generation.
func resolveWindowParams(cmd *cobra.Command, basic bool) (uint32, []byte, uint16, error) {
	if basic {
		return 0, nil, 0, nil
	}

	passcode, _ := cmd.Flags().GetUint32("passcode")
	if cmd.Flags().Changed("passcode") {
		if err := commissioning.ValidatePasscode(passcode); err != nil {
			return 0, nil, 0, err
		}
	} else {
		p, err := commissioning.GenerateRandomPasscode()
		if err != nil {
			return 0, nil, 0, fmt.Errorf("generating random passcode: %w", err)
		}
		passcode = p
	}

	disc, _ := cmd.Flags().GetUint16("discriminator")
	if !cmd.Flags().Changed("discriminator") {
		d, err := commissioning.GenerateRandomDiscriminator()
		if err != nil {
			return 0, nil, 0, fmt.Errorf("generating random discriminator: %w", err)
		}
		disc = d
	} else if disc > commissioning.MaxDiscriminator {
		return 0, nil, 0, fmt.Errorf("--discriminator %d exceeds 12 bits", disc)
	}

	var salt []byte
	if saltHex, _ := cmd.Flags().GetString("salt-hex"); saltHex != "" {
		b, err := hex.DecodeString(strings.TrimPrefix(saltHex, "0x"))
		if err != nil {
			return 0, nil, 0, fmt.Errorf("--salt-hex is not valid hex: %w", err)
		}
		if len(b) < commissioning.MinSaltLength || len(b) > commissioning.MaxSaltLength {
			return 0, nil, 0, fmt.Errorf("--salt-hex length %d outside [%d,%d]",
				len(b), commissioning.MinSaltLength, commissioning.MaxSaltLength)
		}
		salt = b
	} else {
		s, err := commissioning.GenerateRandomSalt(commissioning.MinSaltLength)
		if err != nil {
			return 0, nil, 0, fmt.Errorf("generating random salt: %w", err)
		}
		salt = s
	}

	return passcode, salt, disc, nil
}

// encodeOpenWindowFields builds the TLV payload for OpenCommissioningWindow.
// Tags (per AdministratorCommissioning spec):
//
//	0: CommissioningTimeout (uint16)
//	1: PAKEPasscodeVerifier (octets, 97 bytes)
//	2: Discriminator (uint16)
//	3: Iterations (uint32)
//	4: Salt (octets)
func encodeOpenWindowFields(timeoutSecs uint16, verifier []byte, disc uint16, iterations uint32, salt []byte) ([]byte, error) {
	w := tlv.NewWriter()
	if err := w.PutUnsignedInt(tlv.ContextTag(0), uint64(timeoutSecs)); err != nil {
		return nil, err
	}
	if err := w.PutOctetString(tlv.ContextTag(1), verifier); err != nil {
		return nil, err
	}
	if err := w.PutUnsignedInt(tlv.ContextTag(2), uint64(disc)); err != nil {
		return nil, err
	}
	if err := w.PutUnsignedInt(tlv.ContextTag(3), uint64(iterations)); err != nil {
		return nil, err
	}
	if err := w.PutOctetString(tlv.ContextTag(4), salt); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

// encodeOpenBasicFields builds the TLV payload for OpenBasicCommissioningWindow.
// Tag 0: CommissioningTimeout (uint16).
func encodeOpenBasicFields(timeoutSecs uint16) ([]byte, error) {
	w := tlv.NewWriter()
	if err := w.PutUnsignedInt(tlv.ContextTag(0), uint64(timeoutSecs)); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

// invokeAdminCommissioning issues a Timed Invoke of the given Administrator
// Commissioning command on endpoint 0. It prefers the session daemon when
// available (reusing an already-established CASE session) and falls back to
// a direct CASE connection. Cluster-specific status codes are decoded into
// human-friendly errors.
func invokeAdminCommissioning(ctx context.Context, stepper *output.Stepper, nodeID uint64, commandID uint32, fields []byte) error {
	const (
		endpoint  uint16 = 0
		clusterID uint32 = administratorcommissioning.ID
	)

	if dc, ok := connectViaDaemon(nodeID); ok {
		slog.Debug("using session daemon for admin commissioning invoke", "node", nodeID, "cmd", commandID)
		stepper.Step("Sending Timed Invoke " + output.Muted("(via session daemon)"))
		resp, err := dc.Invoke(endpoint, clusterID, commandID, fields, timedInteractionTimeoutMs)
		if err != nil {
			stepper.Fail(fmt.Sprintf("Invoke failed: %v", err))
			return fmt.Errorf("invoking command: %w", err)
		}
		if resp.StatusCode != 0 || resp.ClusterStatus != nil {
			return handleAdminStatus(stepper, commandID, resp.StatusCode, resp.ClusterStatus)
		}
		return nil
	}

	client, session, cleanup, err := connectToNode(ctx, nodeID)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Failed to connect: %v", err))
		return err
	}
	defer cleanup()

	return invokeAdminCommissioningDirect(ctx, stepper, client, session, endpoint, clusterID, commandID, fields)
}

// invokeAdminCommissioningDirect runs a Timed Invoke over a caller-owned CASE
// session and decodes the response, mapping cluster-specific status codes to
// friendly errors.
func invokeAdminCommissioningDirect(ctx context.Context, stepper *output.Stepper, client *interaction.Client, session *protocol.Session, endpoint uint16, clusterID, commandID uint32, fields []byte) error {
	path := interaction.NewCommandPath(endpoint, clusterID, commandID)
	resp, err := client.InvokeTimed(ctx, session, path, fields, timedInteractionTimeoutMs)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Invoke failed: %v", err))
		return fmt.Errorf("invoking command: %w", err)
	}
	if resp.Status != nil {
		st := resp.Status.Status
		if st.Status != 0 || st.ClusterStatus != nil {
			return handleAdminStatus(stepper, commandID, st.Status, st.ClusterStatus)
		}
	}
	return nil
}

// handleAdminStatus turns IM / cluster-specific status codes from the
// Administrator Commissioning cluster into user-friendly errors. Returns nil
// for an explicit Success (0,nil).
func handleAdminStatus(stepper *output.Stepper, commandID uint32, imStatus uint8, clusterStatus *uint8) error {
	if imStatus == 0 && clusterStatus == nil {
		return nil
	}

	var msg string
	switch {
	case clusterStatus != nil:
		switch *clusterStatus {
		case adminBusy:
			msg = "device is busy (Busy) — a commissioning window is already open"
		case adminPAKEParamError:
			msg = "device rejected the PAKE parameters (PAKEParameterError)"
		case adminWindowNotOpen:
			if commandID == administratorcommissioning.CmdRevokeCommissioning {
				msg = "no commissioning window is currently open (WindowNotOpen)"
			} else {
				msg = "WindowNotOpen"
			}
		default:
			msg = fmt.Sprintf("cluster status 0x%02X", *clusterStatus)
		}
	default:
		msg = fmt.Sprintf("%s (0x%02X)", interaction.StatusCode(imStatus), imStatus)
	}

	stepper.Fail(fmt.Sprintf("Command failed: %s", msg))
	return fmt.Errorf("%s", msg)
}

// loadNodeVIDPID returns the stored VendorID/ProductID for a node, falling back
// to reading BasicInformation over CASE when they are not persisted. Read
// errors are non-fatal: the returned values default to zero.
func loadNodeVIDPID(ctx context.Context, nodeID uint64, stepper *output.Stepper) (uint16, uint16) {
	fabricID := viper.GetUint64("default-fabric-id")
	if fabricID == 0 {
		fabricID = 1
	}

	var node *store.Node
	if n, err := getNodeForCompletion(fabricID, nodeID); err == nil {
		node = n
	}
	if node != nil && (node.VendorID != 0 || node.ProductID != 0) {
		return node.VendorID, node.ProductID
	}

	stepper.Step("Reading BasicInformation VID/PID")
	vid, pid, err := readBasicInfoVIDPID(ctx, nodeID)
	if err != nil {
		slog.Debug("could not read BasicInformation VID/PID", "node", nodeID, "err", err)
		return 0, 0
	}
	return vid, pid
}

// readBasicInfoVIDPID reads VendorID (0x0002) and ProductID (0x0004) from
// BasicInformation (cluster 0x0028, endpoint 0). Tries the daemon first, then
// a direct CASE session.
func readBasicInfoVIDPID(ctx context.Context, nodeID uint64) (uint16, uint16, error) {
	const (
		basicInfo = uint32(0x0028)
		attrVID   = uint32(0x0002)
		attrPID   = uint32(0x0004)
		ep0       = uint16(0)
	)

	if dc, ok := connectViaDaemon(nodeID); ok {
		resp, err := dc.Read(
			daemon.AttrPathReq{Endpoint: ep0, ClusterID: basicInfo, AttributeID: attrVID},
			daemon.AttrPathReq{Endpoint: ep0, ClusterID: basicInfo, AttributeID: attrPID},
		)
		if err != nil {
			return 0, 0, fmt.Errorf("daemon read: %w", err)
		}
		var vid, pid uint16
		for _, r := range resp.Reports {
			if r.StatusCode != 0 {
				continue
			}
			data, derr := daemon.DecodeFields(r.Data)
			if derr != nil {
				continue
			}
			switch r.AttributeID {
			case attrVID:
				vid = decodeTLVUint16(data)
			case attrPID:
				pid = decodeTLVUint16(data)
			}
		}
		return vid, pid, nil
	}

	client, session, cleanup, err := connectToNode(ctx, nodeID)
	if err != nil {
		return 0, 0, err
	}
	defer cleanup()

	reports, err := client.Read(ctx, session,
		interaction.NewAttributePath(ep0, basicInfo, attrVID),
		interaction.NewAttributePath(ep0, basicInfo, attrPID),
	)
	if err != nil {
		return 0, 0, err
	}
	var vid, pid uint16
	for _, r := range reports {
		if r.Data == nil || r.Data.Path.AttributeID == nil {
			continue
		}
		switch *r.Data.Path.AttributeID {
		case attrVID:
			vid = decodeTLVUint16(r.Data.Data)
		case attrPID:
			pid = decodeTLVUint16(r.Data.Data)
		}
	}
	return vid, pid, nil
}

// decodeTLVUint16 extracts a uint16 from a raw TLV element.
func decodeTLVUint16(data []byte) uint16 {
	if len(data) == 0 {
		return 0
	}
	r := tlv.NewReader(bytes.NewReader(data))
	if err := r.Next(); err != nil {
		return 0
	}
	switch v := r.Value().(type) {
	case uint64:
		return uint16(v)
	case int64:
		return uint16(v)
	}
	return 0
}
