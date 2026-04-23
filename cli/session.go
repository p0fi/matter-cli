// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	rootCmd.AddCommand(withGroup(newSessionCmd(), groupTools))
}

// newSessionCmd creates the `matter session` subcommand group for managing the
// background session daemon.
func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage the background session daemon",
		Long: `The session daemon keeps CASE sessions alive in the background so that
subsequent CLI invocations reuse an existing session instead of performing a
full CASE handshake each time. This is especially useful for constrained
devices where session establishment can be slow.

Start the daemon with a keep-alive timeout, and all subsequent commands to
the same node will be routed through the daemon automatically.`,
	}
	cmd.AddCommand(newSessionStartCmd())
	cmd.AddCommand(newSessionStopCmd())
	cmd.AddCommand(newSessionStatusCmd())
	return cmd
}

// newSessionStartCmd creates `matter session start`.
func newSessionStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the background session daemon",
		Long: `Starts a background daemon process that caches CASE sessions.
The daemon auto-exits after the specified idle timeout if no commands are
received. While it is running, any matter CLI command that needs to talk to a
node will use the daemon's cached session instead of establishing a new one.`,
		Example: `  matter session start --timeout 5m
  matter session start --timeout 30m
  matter session start --timeout 1h`,
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout, _ := cmd.Flags().GetString("timeout")
			if timeout == "" {
				timeout = "5m"
			}

			dur, err := time.ParseDuration(timeout)
			if err != nil {
				return fmt.Errorf("invalid timeout %q: %w", timeout, err)
			}

			w := cmd.OutOrStdout()

			// Check if daemon is already running.
			client := daemon.NewClient("")
			if client.IsRunning() {
				status, err := client.Status()
				if err == nil {
					fmt.Fprintf(w, "%s Session daemon is already running %s\n",
						output.SuccessIcon(),
						output.Muted(fmt.Sprintf("(uptime: %s, %d cached sessions)",
							status.Uptime.D().Round(time.Second), len(status.Sessions))))
					return nil
				}
			}

			// Start the daemon as a background process by re-executing ourselves
			// with the hidden __daemon subcommand.
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("finding executable path: %w", err)
			}

			daemonCmd := exec.Command(exe, "__daemon",
				"--timeout", dur.String(),
			)
			// Detach from the terminal: redirect stdin/stdout/stderr to /dev/null.
			daemonCmd.Stdin = nil
			daemonCmd.Stdout = nil
			daemonCmd.Stderr = nil
			// Start the daemon in its own process group so it survives the CLI
			// process exiting.
			daemonCmd.SysProcAttr = daemonSysProcAttr()

			if err := daemonCmd.Start(); err != nil {
				return fmt.Errorf("starting daemon process: %w", err)
			}

			// Wait briefly for the daemon to start listening.
			fmt.Fprintf(w, "%s Starting session daemon %s\n",
				output.SpinnerIcon(),
				output.Muted(fmt.Sprintf("(idle timeout: %s)", dur)))

			ready := waitForDaemon(client, 3*time.Second)
			if !ready {
				fmt.Fprintf(w, "%s Daemon process started (pid %d) but socket not yet ready\n",
					output.WarningIcon(), daemonCmd.Process.Pid)
				return nil
			}

			fmt.Fprintf(w, "%s Session daemon started %s\n",
				output.SuccessIcon(),
				output.Muted(fmt.Sprintf("(pid %d, idle timeout: %s)", daemonCmd.Process.Pid, dur)))
			return nil
		},
	}
	cmd.Flags().StringP("timeout", "t", "5m", "idle timeout before the daemon auto-exits (e.g. 5m, 30m, 1h)")
	return cmd
}

// newSessionStopCmd creates `matter session stop`.
func newSessionStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the background session daemon",
		Example: `  matter session stop`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			client := daemon.NewClient("")

			if !client.IsRunning() {
				fmt.Fprintf(w, "%s No session daemon is running\n", output.Muted("●"))
				return nil
			}

			if err := client.Shutdown(); err != nil {
				return fmt.Errorf("shutting down daemon: %w", err)
			}

			// Wait for it to actually exit.
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if !daemon.IsProcessRunning() {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}

			fmt.Fprintf(w, "%s Session daemon stopped\n", output.SuccessIcon())
			return nil
		},
	}
}

// newSessionStatusCmd creates `matter session status`.
func newSessionStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the status of the background session daemon",
		Example: `  matter session status
  matter session status --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			client := daemon.NewClient("")

			if !client.IsRunning() {
				fmt.Fprintf(w, "%s Session daemon is %s\n",
					output.Muted("●"), output.Error("not running"))
				fmt.Fprintf(w, "\n  Start it with: %s\n", output.Bold("matter session start"))
				return nil
			}

			status, err := client.Status()
			if err != nil {
				return fmt.Errorf("getting daemon status: %w", err)
			}

			format, _ := cmd.Flags().GetString("format")
			f := output.New(format)

			// JSON/YAML output.
			if _, isTable := f.(*output.TableFormatter); !isTable {
				return f.Format(w, status)
			}

			// Styled table output.
			fmt.Fprintf(w, "%s Session daemon is %s\n",
				output.SuccessIcon(), output.Success("running"))
			fmt.Fprintf(w, "  %s  %s\n", output.Label("Uptime:"),
				output.Value(status.Uptime.D().Round(time.Second).String()))
			fmt.Fprintf(w, "  %s %s\n", output.Label("Timeout:"),
				output.Value(status.IdleTimeout.D().Round(time.Second).String()))

			if len(status.Sessions) == 0 {
				fmt.Fprintf(w, "\n  %s\n", output.Muted("No cached sessions"))
			} else {
				fmt.Fprintf(w, "\n%s\n", output.Header("  Cached Sessions:"))
				td := &output.TableData{
					Headers: []string{"NODE", "SESSION", "ADDRESS", "AGE"},
				}
				for _, s := range status.Sessions {
					td.Rows = append(td.Rows, []string{
						fmt.Sprintf("%d", s.NodeID),
						fmt.Sprintf("%d", s.SessionID),
						s.PeerAddress,
						s.Established.D().Round(time.Second).String(),
					})
				}
				return f.Format(w, td)
			}
			return nil
		},
	}
}

// newDaemonCmd creates the hidden `__daemon` subcommand that runs the session
// daemon in the foreground. This is invoked by `session start` as a detached
// background process — users should not call it directly.
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "__daemon",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout, _ := cmd.Flags().GetString("timeout")
			dur, err := time.ParseDuration(timeout)
			if err != nil {
				dur = 5 * time.Minute
			}

			s, err := openStore()
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}

			fabricID := viper.GetUint64("default-fabric-id")
			if fabricID == 0 {
				fabricID = 1
			}

			srv := daemon.NewServer(daemon.ServerConfig{
				IdleTimeout: dur,
				Store:       s,
				FabricID:    fabricID,
			})

			ctx := context.Background()
			return srv.Run(ctx)
		},
	}
	cmd.Flags().String("timeout", "5m", "")
	return cmd
}

func init() {
	rootCmd.AddCommand(newDaemonCmd())
}

// waitForDaemon polls the daemon until it responds to a ping or the timeout
// elapses. Returns true if the daemon became ready.
func waitForDaemon(client *daemon.Client, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	interval := 50 * time.Millisecond
	for time.Now().Before(deadline) {
		if client.IsRunning() {
			return true
		}
		time.Sleep(interval)
		// Back off slightly.
		if interval < 200*time.Millisecond {
			interval += 25 * time.Millisecond
		}
	}
	return false
}

// connectViaDaemon checks whether the session daemon is running and returns a
// daemonNodeConn that routes operations through it. Returns nil, false if the
// daemon is not available.
func connectViaDaemon(nodeID uint64) (*daemonNodeConn, bool) {
	client := daemon.NewClient("")
	if !client.IsRunning() {
		return nil, false
	}

	fabricID := viper.GetUint64("default-fabric-id")
	if fabricID == 0 {
		fabricID = 1
	}

	return &daemonNodeConn{
		client:   client,
		nodeID:   nodeID,
		fabricID: fabricID,
	}, true
}

// daemonNodeConn wraps a daemon.Client bound to a specific node, exposing
// invoke/read/write methods that match the calling conventions used by the CLI
// command handlers.
type daemonNodeConn struct {
	client   *daemon.Client
	nodeID   uint64
	fabricID uint64
}

// Invoke sends a command invoke through the daemon.
func (d *daemonNodeConn) Invoke(endpoint uint16, clusterID, commandID uint32, fields []byte, timedMs uint16) (*daemon.InvokeResp, error) {
	return d.client.Invoke(d.nodeID, d.fabricID, &daemon.InvokeReq{
		Endpoint:  endpoint,
		ClusterID: clusterID,
		CommandID: commandID,
		Fields:    daemon.EncodeFields(fields),
		TimedMs:   timedMs,
	})
}

// Read sends an attribute read through the daemon.
func (d *daemonNodeConn) Read(paths ...daemon.AttrPathReq) (*daemon.ReadResp, error) {
	return d.client.Read(d.nodeID, d.fabricID, &daemon.ReadReq{Paths: paths})
}

// Write sends an attribute write through the daemon.
func (d *daemonNodeConn) Write(writes ...daemon.AttrWriteReq) (*daemon.WriteResp, error) {
	return d.client.Write(d.nodeID, d.fabricID, &daemon.WriteReq{Writes: writes})
}
