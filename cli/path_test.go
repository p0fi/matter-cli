// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"testing"
)

func TestExpandUserTilde(t *testing.T) {
	const fakeHome = "/home/tester"

	succeed := func() (string, error) { return fakeHome, nil }

	tests := []struct {
		name    string
		path    string
		goos    string
		home    homeDirFunc
		want    string
		wantErr bool
	}{
		{"bare tilde unix", "~", "linux", succeed, fakeHome, false},
		{"bare tilde windows", "~", "windows", succeed, fakeHome, false},
		{"tilde slash unix", "~/Desktop/tree.svg", "linux", succeed, fakeHome + "/Desktop/tree.svg", false},
		{"tilde slash windows", "~/Desktop/tree.svg", "windows", succeed, fakeHome + "/Desktop/tree.svg", false},
		{"tilde backslash windows", `~\Desktop\tree.svg`, "windows", succeed, fakeHome + `\Desktop\tree.svg`, false},
		{"tilde backslash unix stays literal", `~\Desktop\tree.svg`, "linux", succeed, `~\Desktop\tree.svg`, false},
		{"ordinary relative path", "tree.svg", "linux", succeed, "tree.svg", false},
		{"ordinary absolute path", "/var/tmp/tree.svg", "linux", succeed, "/var/tmp/tree.svg", false},
		{"absolute path with tilde directory unchanged", "/~/Desktop/rac_2.svg", "linux", succeed, "/~/Desktop/rac_2.svg", false},
		{"other user tilde unchanged", "~alice/file.svg", "linux", succeed, "~alice/file.svg", false},
		{"embedded tilde unchanged", "foo~/bar.svg", "linux", succeed, "foo~/bar.svg", false},
		{"env var reference unchanged", "$HOME/tree.svg", "linux", succeed, "$HOME/tree.svg", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calledHome := false
			home := func() (string, error) {
				calledHome = true
				return tt.home()
			}

			got, err := expandUserTilde(tt.path, tt.goos, home)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("expandUserTilde(%q, %q) = %q, want %q", tt.path, tt.goos, got, tt.want)
			}

			wantCalled := tt.path == "~" || (tt.path != tt.want)
			if calledHome != wantCalled {
				t.Errorf("home lookup called = %v, want %v", calledHome, wantCalled)
			}
		})
	}
}

func TestExpandUserTilde_HomeLookupError(t *testing.T) {
	wantErr := errors.New("no home directory")
	_, err := expandUserTilde("~/tree.svg", "linux", func() (string, error) {
		return "", wantErr
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want wrapping %v", err, wantErr)
	}
}

func TestExpandUserTilde_EmptyHomeRejected(t *testing.T) {
	_, err := expandUserTilde("~/tree.svg", "linux", func() (string, error) {
		return "", nil
	})
	if err == nil {
		t.Fatal("expected error for empty home directory, got nil")
	}
}

func TestExpandUserTilde_UnchangedPathSkipsLookup(t *testing.T) {
	called := false
	home := func() (string, error) {
		called = true
		return "/home/tester", nil
	}

	got, err := expandUserTilde("relative/tree.svg", "linux", home)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "relative/tree.svg" {
		t.Errorf("got %q, want unchanged path", got)
	}
	if called {
		t.Error("home lookup should not be called for an unchanged path")
	}
}
