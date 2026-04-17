// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"testing"
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
