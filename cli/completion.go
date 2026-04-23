// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/spf13/cobra"
)

// supportedShells is the single source of truth for shells that matter completion supports.
// Used for both ValidArgs and error messages in detectShell.
var supportedShells = []string{"bash", "zsh", "fish", "powershell"}

// newCompletionCmd creates the `matter completion` subcommand that generates
// and optionally installs shell completion scripts for bash, zsh, fish, and
// powershell.
func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate or install shell completion scripts",
		Long: `Generate shell completion scripts for matter.

With no arguments, matter completion auto-detects your shell from $SHELL and
installs the completion script automatically. On Windows, no-argument
invocation always installs PowerShell completions regardless of $SHELL.

Specify a shell explicitly to print its completion script to stdout. Add
--install to install explicitly for a given shell.`,
		Example: `  # Auto-detect shell and install completions (recommended)
  matter completion

  # On Windows: always installs PowerShell completions
  matter completion

  # Print zsh completions to stdout
  matter completion zsh

  # Install zsh completions (writes file + configures shell)
  matter completion zsh --install

  # Install bash completions
  matter completion bash --install

  # Install fish completions
  matter completion fish --install`,
		ValidArgs: supportedShells,
		Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		RunE:      runCompletion,
	}

	cmd.Flags().Bool("install", false, "install the completion script to the appropriate location")

	return cmd
}

func runCompletion(cmd *cobra.Command, args []string) error {
	install, _ := cmd.Flags().GetBool("install")

	if len(args) == 0 {
		shell, err := detectShell()
		if err != nil {
			return err
		}
		return installCompletion(cmd, shell)
	}

	shell := args[0]
	if !install {
		return generateCompletion(cmd, shell)
	}
	return installCompletion(cmd, shell)
}

// detectShell returns the name of the current user's shell by inspecting $SHELL.
// On Windows it defaults to "powershell". Returns an error when the shell
// cannot be detected or is not among the supported set.
func detectShell() (string, error) {
	if runtime.GOOS == "windows" {
		return "powershell", nil
	}
	shellEnv := os.Getenv("SHELL")
	if shellEnv == "" {
		return "", fmt.Errorf("could not detect your shell — please specify one: %s", strings.Join(supportedShells, ", "))
	}
	name := filepath.Base(shellEnv)
	switch name {
	case "bash", "zsh", "fish":
		return name, nil
	default:
		return "", fmt.Errorf("unsupported shell %q — please specify one: %s", name, strings.Join(supportedShells, ", "))
	}
}

// generateCompletion prints the completion script to stdout.
func generateCompletion(cmd *cobra.Command, shell string) error {
	root := cmd.Root()
	w := cmd.OutOrStdout()

	switch shell {
	case "bash":
		return root.GenBashCompletionV2(w, true)
	case "zsh":
		return genZshCompletion(root, w)
	case "fish":
		return root.GenFishCompletion(w, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(w)
	default:
		return fmt.Errorf("unsupported shell: %s", shell)
	}
}

// installCompletion writes the completion script to the correct location for
// the given shell and prints post-install instructions if needed.
func installCompletion(cmd *cobra.Command, shell string) error {
	root := cmd.Root()
	w := cmd.ErrOrStderr()

	var buf bytes.Buffer
	switch shell {
	case "bash":
		if err := root.GenBashCompletionV2(&buf, true); err != nil {
			return fmt.Errorf("generating bash completion: %w", err)
		}
	case "zsh":
		if err := genZshCompletion(root, &buf); err != nil {
			return fmt.Errorf("generating zsh completion: %w", err)
		}
	case "fish":
		if err := root.GenFishCompletion(&buf, true); err != nil {
			return fmt.Errorf("generating fish completion: %w", err)
		}
	case "powershell":
		if err := root.GenPowerShellCompletionWithDesc(&buf); err != nil {
			return fmt.Errorf("generating powershell completion: %w", err)
		}
	default:
		return fmt.Errorf("unsupported shell: %s", shell)
	}

	destPath, err := completionInstallPath(shell)
	if err != nil {
		return fmt.Errorf("determining install path for %s: %w", shell, err)
	}

	// Create parent directories.
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	if err := os.WriteFile(destPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing completion script to %s: %w", destPath, err)
	}

	fmt.Fprintf(w, "%s Completion script installed to %s\n",
		output.SuccessIcon(), output.Bold(destPath))

	// Handle any extra steps (e.g. sourcing in rc file).
	hint, modified, err := postInstallAction(shell, destPath)
	if err != nil {
		fmt.Fprintf(w, "%s Could not update shell config automatically: %v\n",
			output.WarningIcon(), err)
		fmt.Fprintf(w, "  %s\n", output.Dim(hint))
	} else if modified != "" {
		fmt.Fprintf(w, "%s Updated %s\n", output.SuccessIcon(), output.Bold(modified))
	}

	if hint != "" && err == nil && modified == "" {
		// Hint is informational only (e.g. fish needs no rc changes).
		fmt.Fprintf(w, "  %s\n", output.Dim(hint))
	}

	fmt.Fprintf(w, "\n  %s\n", output.Dim("Restart your shell or open a new terminal to activate completions."))

	return nil
}

// completionInstallPath returns the filesystem path where the completion script
// should be written for the given shell.
func completionInstallPath(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}

	switch shell {
	case "bash":
		return bashInstallPath(home), nil
	case "zsh":
		return zshInstallPath(home), nil
	case "fish":
		return fishInstallPath(home), nil
	case "powershell":
		return powershellInstallPath(home), nil
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
}

// bashInstallPath returns the path for bash completion scripts.
//
// On macOS with Homebrew, prefers the Homebrew bash-completion directory.
// On Linux, prefers /usr/local/share/bash-completion/completions/ if writable.
// Falls back to ~/.config/matter-cli/completion.bash.
func bashInstallPath(home string) string {
	// Check Homebrew bash-completion directory (macOS).
	if runtime.GOOS == "darwin" {
		brewPaths := []string{
			"/opt/homebrew/etc/bash_completion.d",
			"/usr/local/etc/bash_completion.d",
		}
		for _, p := range brewPaths {
			if isWritableDir(p) {
				return filepath.Join(p, "matter")
			}
		}
	}

	// Check system bash-completion directory (Linux).
	if runtime.GOOS == "linux" {
		sysPaths := []string{
			"/usr/local/share/bash-completion/completions",
			"/usr/share/bash-completion/completions",
			"/etc/bash_completion.d",
		}
		for _, p := range sysPaths {
			if isWritableDir(p) {
				return filepath.Join(p, "matter")
			}
		}
	}

	// Fall back to user-local directory.
	return filepath.Join(home, ".config", "matter-cli", "completion.bash")
}

// zshInstallPath returns the path for zsh completion scripts.
//
// If oh-my-zsh is installed (~/.oh-my-zsh exists), uses
// ~/.oh-my-zsh/completions/_matter which is already in fpath by default.
// Otherwise falls back to ~/.zsh/completions/_matter.
func zshInstallPath(home string) string {
	omzDir := filepath.Join(home, ".oh-my-zsh", "completions")
	if isOhMyZsh(home) {
		return filepath.Join(omzDir, "_matter")
	}
	return filepath.Join(home, ".zsh", "completions", "_matter")
}

// isOhMyZsh returns true if oh-my-zsh appears to be installed under home.
func isOhMyZsh(home string) bool {
	info, err := os.Stat(filepath.Join(home, ".oh-my-zsh"))
	return err == nil && info.IsDir()
}

// fishInstallPath returns the path for fish completion scripts.
// Fish auto-loads completions from ~/.config/fish/completions/.
func fishInstallPath(home string) string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "fish", "completions", "matter.fish")
	}
	return filepath.Join(home, ".config", "fish", "completions", "matter.fish")
}

// powershellInstallPath returns the path for the PowerShell completion script.
// Writes to the matter-cli config directory; the profile will be updated to source it.
func powershellInstallPath(home string) string {
	return filepath.Join(home, ".config", "matter-cli", "completion.ps1")
}

// postInstallAction performs any additional setup needed after writing the
// completion script (e.g. adding a source line to a shell rc file).
//
// Returns:
//   - hint: a human-readable string describing what the user should do (or what was done)
//   - modified: the path of the rc file that was modified, or "" if none
//   - err: any error that occurred during the action
func postInstallAction(shell, scriptPath string) (hint string, modified string, err error) {
	switch shell {
	case "bash":
		return bashPostInstall(scriptPath)
	case "zsh":
		return zshPostInstall(scriptPath)
	case "fish":
		// Fish auto-discovers completions from its completions dir — nothing to do.
		return "Fish will auto-load completions from this location.", "", nil
	case "powershell":
		return powershellPostInstall(scriptPath)
	default:
		return "", "", nil
	}
}

// bashPostInstall ensures the bash completion script is sourced from ~/.bashrc.
func bashPostInstall(scriptPath string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}

	// If the script was installed to a system/Homebrew bash-completion directory,
	// it will be sourced automatically — no rc modification needed.
	if !strings.Contains(scriptPath, ".config"+string(os.PathSeparator)+"matter-cli") {
		return "Bash completions will be loaded automatically by bash-completion.", "", nil
	}

	sourceLine := fmt.Sprintf("source %q", scriptPath)
	rcPath := filepath.Join(home, ".bashrc")

	return ensureLineInFile(rcPath, sourceLine, "# matter-cli shell completion")
}

// zshPostInstall ensures the zsh fpath and compinit are configured in ~/.zshrc.
//
// When oh-my-zsh is detected (the script was installed into
// ~/.oh-my-zsh/completions/), no .zshrc changes are needed because oh-my-zsh
// already adds that directory to fpath and runs compinit.
func zshPostInstall(scriptPath string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}

	// oh-my-zsh handles fpath and compinit automatically.
	omzDir := filepath.Join(home, ".oh-my-zsh", "completions")
	if filepath.Dir(scriptPath) == omzDir {
		return "oh-my-zsh will load completions automatically.", "", nil
	}

	completionsDir := filepath.Dir(scriptPath)
	fpathLine := fmt.Sprintf("fpath=(%s $fpath)", completionsDir)
	rcPath := filepath.Join(home, ".zshrc")

	// Check if the completions dir is already in fpath via .zshrc.
	rcContent, rcErr := os.ReadFile(rcPath)
	if rcErr != nil && !os.IsNotExist(rcErr) {
		return fmt.Sprintf("Add this to your ~/.zshrc:\n    %s\n    autoload -Uz compinit && compinit", fpathLine), "", rcErr
	}

	lines := string(rcContent)
	needsFpath := !strings.Contains(lines, completionsDir)
	needsCompinit := !strings.Contains(lines, "compinit")

	if !needsFpath && !needsCompinit {
		return "Your ~/.zshrc already has the fpath and compinit configured.", "", nil
	}

	var additions []string
	if needsFpath {
		additions = append(additions, fpathLine)
	}
	if needsCompinit {
		additions = append(additions, "autoload -Uz compinit && compinit")
	}

	block := "\n# matter-cli shell completion\n" + strings.Join(additions, "\n") + "\n"

	f, err := os.OpenFile(rcPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		hint := fmt.Sprintf("Add this to your ~/.zshrc:\n    %s", strings.Join(additions, "\n    "))
		return hint, "", err
	}
	defer f.Close()

	if _, err := f.WriteString(block); err != nil {
		hint := fmt.Sprintf("Add this to your ~/.zshrc:\n    %s", strings.Join(additions, "\n    "))
		return hint, "", err
	}

	return "", rcPath, nil
}

// powershellPostInstall ensures the completion script is sourced from the
// PowerShell profile.
func powershellPostInstall(scriptPath string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}

	// Determine PowerShell profile path.
	var profilePath string
	switch runtime.GOOS {
	case "windows":
		profilePath = filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	default:
		profilePath = filepath.Join(home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")
	}

	// Use forward slashes in the source line for cross-platform compatibility.
	sourceLine := fmt.Sprintf(". %q", filepath.ToSlash(scriptPath))

	return ensureLineInFile(profilePath, sourceLine, "# matter-cli shell completion")
}

// ensureLineInFile appends line to the file at path if it is not already
// present. The comment is prepended as a section header on first addition.
//
// Returns:
//   - hint: description of what was or should be done
//   - modified: path of file that was modified, or "" if not modified
//   - err: any error
func ensureLineInFile(path, line, comment string) (string, string, error) {
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		hint := fmt.Sprintf("Add this to %s:\n    %s", path, line)
		return hint, "", err
	}

	// Already present — nothing to do.
	if strings.Contains(string(content), line) {
		return fmt.Sprintf("Already configured in %s.", path), "", nil
	}

	block := fmt.Sprintf("\n%s\n%s\n", comment, line)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		hint := fmt.Sprintf("Add this to %s:\n    %s", path, line)
		return hint, "", err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		hint := fmt.Sprintf("Add this to %s:\n    %s", path, line)
		return hint, "", err
	}
	defer f.Close()

	if _, err := f.WriteString(block); err != nil {
		hint := fmt.Sprintf("Add this to %s:\n    %s", path, line)
		return hint, "", err
	}

	return "", path, nil
}

// genZshCompletion generates a custom _matter zsh completion function that
// visually groups top-level commands into Device Commands, Cluster Commands,
// and Tools sections. The group membership is computed from each command's
// cobra GroupID at script-generation time and embedded as a static lookup
// table. At completion time the function calls `matter __complete` (which
// applies all dynamic @target-based filtering) and routes each result to the
// correct group using _describe -t, which zsh renders with headers.
//
// For sub-command arguments, flags, and attribute/command names the output
// from `matter __complete` goes into an ungrouped bucket and is presented
// without a section header, identical to cobra's default behaviour.
func genZshCompletion(root *cobra.Command, w io.Writer) error {
	// Build the static group-map lines: command-name → group-tag.
	type groupEntry struct {
		name string
		tag  string
	}
	var entries []groupEntry
	for _, cmd := range root.Commands() {
		if cmd.Hidden {
			continue
		}
		var tag string
		switch cmd.GroupID {
		case groupDevices:
			tag = "device"
		case groupClusters:
			tag = "cluster"
		case groupTools:
			tag = "tool"
		}
		if tag != "" {
			entries = append(entries, groupEntry{cmd.Name(), tag})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	var mapLines []string
	for _, e := range entries {
		mapLines = append(mapLines, fmt.Sprintf("  [%s]=%s", e.name, e.tag))
	}

	script := `#compdef matter
compdef _matter matter

# ── matter zsh completion ────────────────────────────────────────────────────
# Regenerate with:  matter completion zsh [--install]

# Group headers: requires group-name '' to be set (oh-my-zsh sets this
# globally; we set it locally for matter so vanilla zsh also benefits).
zstyle ':completion:*:matter:*' group-name ''
zstyle ':completion:*:matter:*:targets'            format $'\e[35m── Targets ──\e[0m'
zstyle ':completion:*:matter:*:device-commands'    format $'\e[36m── Device Commands ──\e[0m'
zstyle ':completion:*:matter:*:cluster-commands'   format $'\e[32m── Cluster Commands ──\e[0m'
zstyle ':completion:*:matter:*:tools'              format $'\e[33m── Tools ──\e[0m'
zstyle ':completion:*:matter:*:invoke-commands'    format $'\e[32m── Commands ──\e[0m'
zstyle ':completion:*:matter:*:attr-interactions'  format $'\e[34m── Attribute Interactions ──\e[0m'

# Static lookup: command name → group tag (device | cluster | tool).
# Regenerate whenever new clusters or commands are added.
typeset -gA _matter_group_map
_matter_group_map=(
` + strings.Join(mapLines, "\n") + `
)

_matter() {
  local -a request_cmd
  local -a device_cmds cluster_cmds tool_cmds target_cmds other_cmds invoke_cmds attr_cmds
  local out word desc entry tag i directive _matter_line

  # Build the __complete call from the current word list.
  request_cmd=("${words[1]}" "__complete")
  for (( i=2; i<CURRENT; i++ )); do
    request_cmd+=("${words[$i]}")
  done
  request_cmd+=("${words[$CURRENT]}")

  out=$("${request_cmd[@]}" 2>/dev/null)

  # Extract the cobra ShellCompDirective from the trailing :N line so we can
  # honour flags like ShellCompDirectiveNoSpace (bit 1, value 2).
  directive=0
  while IFS= read -r _matter_line; do
    [[ "$_matter_line" == :* ]] && directive="${_matter_line#:}"
  done <<< "$out"

  # Parse output lines, skipping the trailing :N directive and blank lines.
  while IFS=$'\t' read -r word desc; do
    [[ -z "$word" || "$word" == :* || "$word" == _activeHelp_* ]] && continue
    # Escape colons in word and description (zsh _describe uses : as separator).
    entry="${word//:/\\:}:${desc//:/\\:}"
    if [[ "$word" == @* ]]; then
      target_cmds+=("$entry")
    else
      tag="${_matter_group_map[$word]}"
      case "$tag" in
        device)  device_cmds+=("$entry") ;;
        cluster) cluster_cmds+=("$entry") ;;
        tool)    tool_cmds+=("$entry") ;;
        *)
          # Shorthand cluster subcommands are identified by their description:
          # invoke commands use "Invoke <Cluster>.<Command>"; attribute
          # interactions use "Read a <Cluster> attribute" / "Write a <Cluster>
          # attribute". Anything else falls through to the ungrouped bucket.
          case "$desc" in
            "Invoke "*)                      invoke_cmds+=("$entry") ;;
            "Read a "*" attribute"*)         attr_cmds+=("$entry") ;;
            "Write a "*" attribute"*)        attr_cmds+=("$entry") ;;
            *)                                other_cmds+=("$entry") ;;
          esac
          ;;
      esac
    fi
  done <<< "$out"

  # ShellCompDirectiveNoSpace (bit 1, value 2): suppress trailing space so the
  # user can continue typing (e.g. after "Level=" or "@1/").
  local -a nospace
  (( directive & 2 )) && nospace=(-S '')

  (( ${#target_cmds}  )) && _describe -t targets            "Targets"                target_cmds  "${nospace[@]}"
  (( ${#device_cmds}  )) && _describe -t device-commands    "Device Commands"        device_cmds  "${nospace[@]}"
  (( ${#cluster_cmds} )) && _describe -t cluster-commands   "Cluster Commands"       cluster_cmds "${nospace[@]}"
  (( ${#tool_cmds}    )) && _describe -t tools              "Tools"                  tool_cmds    "${nospace[@]}"
  (( ${#invoke_cmds}  )) && _describe -t invoke-commands    "Commands"               invoke_cmds  "${nospace[@]}"
  (( ${#attr_cmds}    )) && _describe -t attr-interactions  "Attribute Interactions" attr_cmds    "${nospace[@]}"
  (( ${#other_cmds}   )) && _describe -t arguments          ""                       other_cmds   "${nospace[@]}"

  return 0
}

if [ "$funcstack[1]" = "_matter" ]; then
  _matter
fi
`
	_, err := io.WriteString(w, script)
	return err
}

// isWritableDir returns true if the path exists, is a directory, and is
// writable by the current process.
func isWritableDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}

	// Try creating a temp file to test write access. This is more reliable
	// than checking permission bits, especially on macOS with SIP.
	tmp := filepath.Join(path, ".matter-cli-write-test")
	f, err := os.Create(tmp)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(tmp)
	return true
}
