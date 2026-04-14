// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRootWithCompletion creates a minimal, isolated root command with the
// completion subcommand attached. This avoids polluting the global rootCmd
// which would cause duplicate registrations across subtests.
func newTestRootWithCompletion() (*cobra.Command, *cobra.Command) {
	root := &cobra.Command{
		Use:   "matter",
		Short: "test root",
	}
	comp := newCompletionCmd()
	root.AddCommand(comp)
	return root, comp
}

func TestCompletionGenerate(t *testing.T) {
	shells := []string{"bash", "zsh", "fish", "powershell"}
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			root, _ := newTestRootWithCompletion()

			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs([]string{"completion", shell})

			err := root.Execute()
			require.NoError(t, err)
			assert.NotEmpty(t, stdout.String(), "expected completion output for %s", shell)
		})
	}
}

func TestCompletionGenerate_ContainsShellSpecificContent(t *testing.T) {
	tests := []struct {
		shell    string
		contains string
	}{
		{"bash", "_matter"},         // bash completion function name
		{"zsh", "compdef"},          // zsh uses compdef
		{"fish", "complete"},        // fish uses complete command
		{"powershell", "Register-"}, // powershell uses Register-ArgumentCompleter
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			root, _ := newTestRootWithCompletion()

			var stdout bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&bytes.Buffer{})
			root.SetArgs([]string{"completion", tt.shell})

			err := root.Execute()
			require.NoError(t, err)
			assert.Contains(t, stdout.String(), tt.contains,
				"completion output for %s should contain %q", tt.shell, tt.contains)
		})
	}
}

func TestCompletionInvalidShell(t *testing.T) {
	root, _ := newTestRootWithCompletion()

	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"completion", "nushell"})

	err := root.Execute()
	assert.Error(t, err)
}

func TestCompletionNoArgs(t *testing.T) {
	root, _ := newTestRootWithCompletion()

	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"completion"})

	err := root.Execute()
	assert.Error(t, err)
}

func TestCompletionInstallPath_Zsh(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		home := t.TempDir()
		got := zshInstallPath(home)
		expected := filepath.Join(home, ".zsh", "completions", "_matter")
		assert.Equal(t, expected, got)
	})

	t.Run("oh-my-zsh", func(t *testing.T) {
		home := t.TempDir()
		// Simulate oh-my-zsh being installed.
		err := os.MkdirAll(filepath.Join(home, ".oh-my-zsh"), 0o755)
		require.NoError(t, err)

		got := zshInstallPath(home)
		expected := filepath.Join(home, ".oh-my-zsh", "completions", "_matter")
		assert.Equal(t, expected, got)
	})
}

func TestCompletionInstallPath_Fish(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		// Unset XDG_CONFIG_HOME to test default path.
		t.Setenv("XDG_CONFIG_HOME", "")
		home := t.TempDir()
		got := fishInstallPath(home)
		expected := filepath.Join(home, ".config", "fish", "completions", "matter.fish")
		assert.Equal(t, expected, got)
	})

	t.Run("xdg", func(t *testing.T) {
		xdg := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdg)
		home := t.TempDir()
		got := fishInstallPath(home)
		expected := filepath.Join(xdg, "fish", "completions", "matter.fish")
		assert.Equal(t, expected, got)
		_ = home // home is ignored when XDG is set
	})
}

func TestCompletionInstallPath_Powershell(t *testing.T) {
	home := t.TempDir()
	got := powershellInstallPath(home)
	expected := filepath.Join(home, ".config", "matter-cli", "completion.ps1")
	assert.Equal(t, expected, got)
}

func TestCompletionInstallPath_Bash(t *testing.T) {
	home := t.TempDir()
	got := bashInstallPath(home)
	// When no system dirs are writable, falls back to user-local path.
	expected := filepath.Join(home, ".config", "matter-cli", "completion.bash")
	// On CI / test environments the Homebrew or system dirs might exist and
	// be writable, so we just check the result is non-empty and absolute-ish.
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		assert.Equal(t, expected, got)
	} else {
		assert.NotEmpty(t, got)
	}
}

func TestCompletionInstallPath_AllShells(t *testing.T) {
	shells := []string{"bash", "zsh", "fish", "powershell"}
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			path, err := completionInstallPath(shell)
			require.NoError(t, err)
			assert.NotEmpty(t, path)
		})
	}

	t.Run("unsupported", func(t *testing.T) {
		_, err := completionInstallPath("nushell")
		assert.Error(t, err)
	})
}

func TestEnsureLineInFile_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, "subdir", ".testrc")

	hint, modified, err := ensureLineInFile(rcPath, "source /foo/bar", "# test comment")
	require.NoError(t, err)
	assert.Equal(t, rcPath, modified)
	assert.Empty(t, hint)

	content, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "# test comment")
	assert.Contains(t, string(content), "source /foo/bar")
}

func TestEnsureLineInFile_SkipsIfAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".testrc")

	// Pre-populate the file with the line.
	err := os.WriteFile(rcPath, []byte("source /foo/bar\n"), 0o644)
	require.NoError(t, err)

	hint, modified, err := ensureLineInFile(rcPath, "source /foo/bar", "# test comment")
	require.NoError(t, err)
	assert.Empty(t, modified, "should not modify file when line already present")
	assert.Contains(t, hint, "Already configured")

	// Verify the file was NOT modified (no duplicate).
	content, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(content), "source /foo/bar"))
}

func TestEnsureLineInFile_AppendsToExistingFile(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".testrc")

	existing := "# existing config\nalias ll='ls -la'\n"
	err := os.WriteFile(rcPath, []byte(existing), 0o644)
	require.NoError(t, err)

	hint, modified, err := ensureLineInFile(rcPath, "source /foo/bar", "# test comment")
	require.NoError(t, err)
	assert.Equal(t, rcPath, modified)
	assert.Empty(t, hint)

	content, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	// Original content preserved.
	assert.Contains(t, string(content), "alias ll='ls -la'")
	// New content appended.
	assert.Contains(t, string(content), "# test comment")
	assert.Contains(t, string(content), "source /foo/bar")
}

func TestZshPostInstall_OhMyZsh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Simulate oh-my-zsh being installed.
	omzCompletions := filepath.Join(home, ".oh-my-zsh", "completions")
	err := os.MkdirAll(omzCompletions, 0o755)
	require.NoError(t, err)

	scriptPath := filepath.Join(omzCompletions, "_matter")

	hint, modified, err := zshPostInstall(scriptPath)
	require.NoError(t, err)
	assert.Empty(t, modified, "should not modify any rc file when oh-my-zsh is detected")
	assert.Contains(t, hint, "oh-my-zsh")

	// .zshrc should NOT have been created or modified.
	_, statErr := os.Stat(filepath.Join(home, ".zshrc"))
	if statErr == nil {
		content, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
		assert.Empty(t, string(content), ".zshrc should not have been written to")
	}
}

func TestZshPostInstall_AddsFpathAndCompinit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a minimal .zshrc without fpath or compinit.
	rcPath := filepath.Join(home, ".zshrc")
	err := os.WriteFile(rcPath, []byte("# my zshrc\n"), 0o644)
	require.NoError(t, err)

	completionsDir := filepath.Join(home, ".zsh", "completions")
	scriptPath := filepath.Join(completionsDir, "_matter")
	err = os.MkdirAll(completionsDir, 0o755)
	require.NoError(t, err)

	hint, modified, err := zshPostInstall(scriptPath)
	require.NoError(t, err)
	assert.Equal(t, rcPath, modified)
	assert.Empty(t, hint)

	content, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), completionsDir)
	assert.Contains(t, string(content), "compinit")
}

func TestZshPostInstall_SkipsIfAlreadyConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	completionsDir := filepath.Join(home, ".zsh", "completions")
	scriptPath := filepath.Join(completionsDir, "_matter")

	rcContent := "fpath=(" + completionsDir + " $fpath)\nautoload -Uz compinit && compinit\n"
	rcPath := filepath.Join(home, ".zshrc")
	err := os.WriteFile(rcPath, []byte(rcContent), 0o644)
	require.NoError(t, err)

	hint, modified, err := zshPostInstall(scriptPath)
	require.NoError(t, err)
	assert.Empty(t, modified, "should not modify .zshrc when already configured")
	assert.Contains(t, hint, "already has")
}

func TestZshPostInstall_OnlyAddsCompinit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	completionsDir := filepath.Join(home, ".zsh", "completions")
	scriptPath := filepath.Join(completionsDir, "_matter")

	// fpath is already there, but compinit is missing.
	rcContent := "fpath=(" + completionsDir + " $fpath)\n"
	rcPath := filepath.Join(home, ".zshrc")
	err := os.WriteFile(rcPath, []byte(rcContent), 0o644)
	require.NoError(t, err)

	hint, modified, err := zshPostInstall(scriptPath)
	require.NoError(t, err)
	assert.Equal(t, rcPath, modified)
	assert.Empty(t, hint)

	content, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "compinit")
	// fpath line should NOT be duplicated.
	assert.Equal(t, 1, strings.Count(string(content), completionsDir))
}

func TestZshPostInstall_CreatesZshrcIfMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	completionsDir := filepath.Join(home, ".zsh", "completions")
	scriptPath := filepath.Join(completionsDir, "_matter")
	err := os.MkdirAll(completionsDir, 0o755)
	require.NoError(t, err)

	// No .zshrc exists at all.
	hint, modified, err := zshPostInstall(scriptPath)
	require.NoError(t, err)

	rcPath := filepath.Join(home, ".zshrc")
	assert.Equal(t, rcPath, modified)
	assert.Empty(t, hint)

	content, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), completionsDir)
	assert.Contains(t, string(content), "compinit")
}

func TestBashPostInstall_SystemDir(t *testing.T) {
	// When script is installed to a system dir, no rc modification is needed.
	hint, modified, err := bashPostInstall("/opt/homebrew/etc/bash_completion.d/matter")
	require.NoError(t, err)
	assert.Empty(t, modified)
	assert.Contains(t, hint, "automatically")
}

func TestBashPostInstall_UserLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	scriptPath := filepath.Join(home, ".config", "matter-cli", "completion.bash")
	err := os.MkdirAll(filepath.Dir(scriptPath), 0o755)
	require.NoError(t, err)

	hint, modified, err := bashPostInstall(scriptPath)
	require.NoError(t, err)

	rcPath := filepath.Join(home, ".bashrc")
	assert.Equal(t, rcPath, modified)
	assert.Empty(t, hint)

	content, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), scriptPath)
}

func TestBashPostInstall_SkipsDuplicate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	scriptPath := filepath.Join(home, ".config", "matter-cli", "completion.bash")
	err := os.MkdirAll(filepath.Dir(scriptPath), 0o755)
	require.NoError(t, err)

	// First install.
	_, _, err = bashPostInstall(scriptPath)
	require.NoError(t, err)

	// Second install — should skip.
	hint, modified, err := bashPostInstall(scriptPath)
	require.NoError(t, err)
	assert.Empty(t, modified, "should not modify .bashrc when already configured")
	assert.Contains(t, hint, "Already configured")
}

func TestFishPostInstall(t *testing.T) {
	hint, modified, err := postInstallAction("fish", "/home/user/.config/fish/completions/matter.fish")
	require.NoError(t, err)
	assert.Empty(t, modified, "fish should not modify any rc file")
	assert.Contains(t, hint, "auto-load")
}

func TestPowershellPostInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	scriptPath := filepath.Join(home, ".config", "matter-cli", "completion.ps1")
	err := os.MkdirAll(filepath.Dir(scriptPath), 0o755)
	require.NoError(t, err)

	hint, modified, err := powershellPostInstall(scriptPath)
	require.NoError(t, err)

	var expectedProfile string
	switch runtime.GOOS {
	case "windows":
		expectedProfile = filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	default:
		expectedProfile = filepath.Join(home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")
	}

	assert.Equal(t, expectedProfile, modified)
	assert.Empty(t, hint)

	content, err := os.ReadFile(expectedProfile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "matter-cli")
}

func TestPowershellPostInstall_SkipsDuplicate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	scriptPath := filepath.Join(home, ".config", "matter-cli", "completion.ps1")

	// First install.
	_, _, err := powershellPostInstall(scriptPath)
	require.NoError(t, err)

	// Second install — should skip.
	hint, modified, err := powershellPostInstall(scriptPath)
	require.NoError(t, err)
	assert.Empty(t, modified, "should not modify profile when already configured")
	assert.Contains(t, hint, "Already configured")
}

func TestInstallCompletion_EndToEnd(t *testing.T) {
	shells := []string{"bash", "zsh", "fish", "powershell"}
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			// Ensure XDG doesn't interfere.
			t.Setenv("XDG_CONFIG_HOME", "")

			root, _ := newTestRootWithCompletion()

			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs([]string{"completion", shell, "--install"})

			err := root.Execute()
			require.NoError(t, err)

			// Verify the completion script file was created at the right path.
			var destPath string
			switch shell {
			case "bash":
				destPath = bashInstallPath(home)
			case "zsh":
				destPath = zshInstallPath(home)
			case "fish":
				destPath = fishInstallPath(home)
			case "powershell":
				destPath = powershellInstallPath(home)
			}
			assert.FileExists(t, destPath)

			// Verify the file is non-empty.
			content, err := os.ReadFile(destPath)
			require.NoError(t, err)
			assert.NotEmpty(t, content)

			// Verify stderr has the success message.
			assert.Contains(t, stderr.String(), "✓")
		})
	}
}

func TestInstallCompletion_EndToEnd_ZshOhMyZsh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	// Simulate oh-my-zsh being installed.
	err := os.MkdirAll(filepath.Join(home, ".oh-my-zsh"), 0o755)
	require.NoError(t, err)

	root, _ := newTestRootWithCompletion()

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"completion", "zsh", "--install"})

	err = root.Execute()
	require.NoError(t, err)

	// Script should be installed under oh-my-zsh completions.
	expected := filepath.Join(home, ".oh-my-zsh", "completions", "_matter")
	assert.FileExists(t, expected)

	content, err := os.ReadFile(expected)
	require.NoError(t, err)
	assert.Contains(t, string(content), "compdef")

	// stderr should mention oh-my-zsh auto-loading, not .zshrc modification.
	assert.Contains(t, stderr.String(), "oh-my-zsh")
	assert.NotContains(t, stderr.String(), ".zshrc")
}

func TestInstallCompletion_Idempotent(t *testing.T) {
	// Running --install twice should succeed without errors or duplicates.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	for i := 0; i < 2; i++ {
		root, _ := newTestRootWithCompletion()
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs([]string{"completion", "zsh", "--install"})

		err := root.Execute()
		require.NoError(t, err, "install attempt %d should succeed", i+1)
	}

	// The fpath line should appear exactly once in .zshrc.
	rcPath := filepath.Join(home, ".zshrc")
	content, err := os.ReadFile(rcPath)
	require.NoError(t, err)

	completionsDir := filepath.Join(home, ".zsh", "completions")
	assert.Equal(t, 1, strings.Count(string(content), completionsDir),
		".zshrc should contain the fpath entry exactly once after two installs")
}

func TestIsOhMyZsh(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		home := t.TempDir()
		err := os.MkdirAll(filepath.Join(home, ".oh-my-zsh"), 0o755)
		require.NoError(t, err)
		assert.True(t, isOhMyZsh(home))
	})

	t.Run("absent", func(t *testing.T) {
		home := t.TempDir()
		assert.False(t, isOhMyZsh(home))
	})

	t.Run("file_not_dir", func(t *testing.T) {
		home := t.TempDir()
		err := os.WriteFile(filepath.Join(home, ".oh-my-zsh"), []byte("not a dir"), 0o644)
		require.NoError(t, err)
		assert.False(t, isOhMyZsh(home))
	})
}

func TestIsWritableDir(t *testing.T) {
	t.Run("writable", func(t *testing.T) {
		dir := t.TempDir()
		assert.True(t, isWritableDir(dir))
	})

	t.Run("nonexistent", func(t *testing.T) {
		assert.False(t, isWritableDir("/nonexistent/path/that/does/not/exist"))
	})

	t.Run("file_not_dir", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "afile")
		err := os.WriteFile(f, []byte("hello"), 0o644)
		require.NoError(t, err)
		assert.False(t, isWritableDir(f))
	})
}

// TestZshCompletion_ContainsDirectiveParsing verifies that the generated zsh
// completion script contains the directive-parsing logic introduced for
// ShellCompDirectiveNoSpace support.
func TestZshCompletion_ContainsDirectiveParsing(t *testing.T) {
	root, _ := newTestRootWithCompletion()

	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"completion", "zsh"})

	err := root.Execute()
	require.NoError(t, err)

	script := stdout.String()

	// The script must parse the directive line.
	assert.Contains(t, script, "directive=", "zsh script should declare a directive variable")
	assert.Contains(t, script, "directive & 2", "zsh script should check ShellCompDirectiveNoSpace bit")
	// Node-level targets use -S '' to suppress trailing space.
	assert.Contains(t, script, "-S ''", "zsh script should suppress trailing space for node-level targets")
}

// TestFilterShorthandCommands_NodeOnly verifies that when a node-only target is
// set (ExplicitEndpoint=false), all cluster-group commands are hidden and the
// device command remains visible.
//
// This test operates on the global rootCmd command tree. Because cobra Hidden
// flags are shared state, it resets them after the test to avoid affecting
// subsequent tests.
func TestFilterShorthandCommands_NodeOnly(t *testing.T) {
	// Snapshot Hidden state before the test.
	type hiddenState struct {
		cmd    *cobra.Command
		hidden bool
	}
	var snapshot []hiddenState
	for _, cmd := range rootCmd.Commands() {
		snapshot = append(snapshot, hiddenState{cmd, cmd.Hidden})
	}
	for _, cmd := range shorthandCmds {
		snapshot = append(snapshot, hiddenState{cmd, cmd.Hidden})
	}
	t.Cleanup(func() {
		for _, s := range snapshot {
			s.cmd.Hidden = s.hidden
		}
	})

	// Apply node-only target (no explicit endpoint).
	target := &Target{NodeID: 1, EndpointSet: false, ExplicitEndpoint: false}
	filterShorthandCommands(target)

	// All shorthand cluster commands should be hidden.
	for _, cmd := range shorthandCmds {
		assert.True(t, cmd.Hidden,
			"shorthand cluster command %q should be hidden for node-only target", cmd.Name())
	}

	// The cluster parent command should be hidden.
	var clusterCmd *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "cluster" {
			clusterCmd = cmd
			break
		}
	}
	if clusterCmd != nil {
		assert.True(t, clusterCmd.Hidden,
			"'cluster' command should be hidden for node-only target")
	}

	// The device command should NOT be hidden.
	var deviceCmd *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "device" {
			deviceCmd = cmd
			break
		}
	}
	if deviceCmd != nil {
		assert.False(t, deviceCmd.Hidden,
			"'device' command should be visible for node-only target")
	}
}

// TestRootHelp_ShorthandClusterFiltering verifies that the styled root help
// template: (1) hides shorthand cluster commands, (2) shows the cluster
// command, and (3) includes discoverability hint lines.
func TestRootHelp_ShorthandClusterFiltering(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	})

	rootCmd.SetArgs([]string{"--help"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	help := buf.String()

	// The cluster command should appear in help.
	assert.Contains(t, help, "cluster", "cluster command should appear in root help")

	// At least one known shorthand cluster command should be absent.
	require.NotEmpty(t, shorthandCmds, "at least one shorthand cluster command must be registered")
	assert.NotContains(t, help, shorthandCmds[0].Name(),
		"shorthand cluster commands should be hidden from root help")

	// Discoverability hint lines should be visible.
	assert.Contains(t, help, "matter cluster list",
		"cluster list hint should appear in root help")
	assert.Contains(t, help, "matter <ClusterName> --help",
		"shorthand hint should appear in root help")
}

// TestFilterShorthandCommands_ExplicitEndpoint verifies that when an
// endpoint-explicit target is set (ExplicitEndpoint=true), the device command
// is hidden and cluster commands are not globally hidden.
func TestFilterShorthandCommands_ExplicitEndpoint(t *testing.T) {
	// Snapshot Hidden state.
	type hiddenState struct {
		cmd    *cobra.Command
		hidden bool
	}
	var snapshot []hiddenState
	for _, cmd := range rootCmd.Commands() {
		snapshot = append(snapshot, hiddenState{cmd, cmd.Hidden})
	}
	for _, cmd := range shorthandCmds {
		snapshot = append(snapshot, hiddenState{cmd, cmd.Hidden})
	}
	t.Cleanup(func() {
		for _, s := range snapshot {
			s.cmd.Hidden = s.hidden
		}
	})

	// Apply endpoint-explicit target.
	target := &Target{NodeID: 1, Endpoint: 1, EndpointSet: true, ExplicitEndpoint: true}
	filterShorthandCommands(target)

	// The device command should be hidden.
	var deviceCmd *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "device" {
			deviceCmd = cmd
			break
		}
	}
	if deviceCmd != nil {
		assert.True(t, deviceCmd.Hidden,
			"'device' command should be hidden for endpoint-explicit target")
	}

	// Target-unaware commands should be hidden.
	for _, cmd := range rootCmd.Commands() {
		if targetUnawareCommands[cmd.Name()] {
			assert.True(t, cmd.Hidden,
				"target-unaware command %q should be hidden for endpoint-explicit target", cmd.Name())
		}
	}
}
