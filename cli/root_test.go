// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestInitConfig_CreatesConfigOnFirstRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	initConfig()

	cfgFile := filepath.Join(tmp, "matter-cli", "config.yaml")
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("config file is empty")
	}
	// Template should contain the actual resolved path.
	if !containsStr(string(data), cfgFile) {
		t.Errorf("config file does not contain its own path; content:\n%s", data)
	}
}

func TestInitConfig_DoesNotOverwriteExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "matter-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfgFile := filepath.Join(dir, "config.yaml")
	original := "# my custom config\nformat: json\n"
	if err := os.WriteFile(cfgFile, []byte(original), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	initConfig()
	initConfig() // second call must also be a no-op

	data, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != original {
		t.Errorf("existing config was overwritten\ngot:  %q\nwant: %q", string(data), original)
	}
}

// TestMaybeStartDaemon_BypassAnnotation verifies that a command carrying
// bypassDaemonAnnotation (subscribe, generic and shorthand) never even
// parses --keep-alive, let alone attempts to spawn a daemon. An invalid
// duration string would make time.ParseDuration fail if the bypass check
// were skipped, so a nil error here proves the early return fired.
func TestMaybeStartDaemon_BypassAnnotation(t *testing.T) {
	cmd := &cobra.Command{
		Use:         "subscribe",
		Annotations: map[string]string{bypassDaemonAnnotation: "true"},
	}
	cmd.Flags().StringP("keep-alive", "K", "not-a-valid-duration", "")

	if err := maybeStartDaemon(cmd); err != nil {
		t.Errorf("maybeStartDaemon() on an annotated command = %v, want nil", err)
	}
}

// TestMaybeStartDaemon_NoAnnotation_StillValidatesKeepAlive verifies that a
// command without the bypass annotation still goes through the normal
// --keep-alive handling (regression guard: the annotation check must not
// accidentally short-circuit every command).
func TestMaybeStartDaemon_NoAnnotation_StillValidatesKeepAlive(t *testing.T) {
	cmd := &cobra.Command{Use: "read"}
	cmd.Flags().StringP("keep-alive", "K", "not-a-valid-duration", "")

	err := maybeStartDaemon(cmd)
	if err == nil {
		t.Fatal("maybeStartDaemon() with an invalid --keep-alive and no bypass annotation = nil, want an error")
	}
	const wantSubstring = "invalid --keep-alive value"
	if !strings.Contains(err.Error(), wantSubstring) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), wantSubstring)
	}
}
