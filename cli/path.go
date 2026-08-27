// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"strings"
)

// homeDirFunc resolves the current user's home directory. Production code
// passes os.UserHomeDir; tests inject deterministic success and failure
// functions instead of depending on the real environment.
type homeDirFunc func() (string, error)

// expandUserTilde applies the CLI's current-user tilde policy to a
// user-supplied file path: a leading "~" or "~/..." (and, when goos is
// "windows", "~\\...") is expanded to the current user's home directory.
// Every other path — including "/~/...", "~username/...", embedded tildes,
// and environment-variable references — is returned unchanged.
//
// This is intentionally narrow: it does not make relative paths absolute,
// resolve symlinks, or create directories. Any CLI flag that accepts a
// filesystem path can reuse it to keep tilde handling consistent.
func expandUserTilde(path, goos string, homeDir homeDirFunc) (string, error) {
	suffix, ok := tildeHomeSuffix(path, goos)
	if !ok {
		return path, nil
	}

	home, err := homeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	if home == "" {
		return "", fmt.Errorf("resolving home directory: got empty path")
	}

	return home + suffix, nil
}

// tildeHomeSuffix reports whether path is exactly "~" or begins with a
// platform-supported "~<separator>" prefix. On success it returns the
// remainder of path (including the leading separator, if any) to append to
// the home directory. Unix recognizes "/"; Windows additionally recognizes
// "\\". Forms like "/~/...", "~username/...", and embedded tildes are not
// matched.
func tildeHomeSuffix(path, goos string) (string, bool) {
	if path == "~" {
		return "", true
	}
	if strings.HasPrefix(path, "~/") {
		return path[1:], true
	}
	if goos == "windows" && strings.HasPrefix(path, `~\`) {
		return path[1:], true
	}
	return "", false
}
