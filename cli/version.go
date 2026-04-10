// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"runtime"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/spf13/cobra"
)

const matterLogo = `
         █
         █
     ▄   █   ▄                                █     █
     ▀▀█████▀▀      ▄▀▀▀▄ ▄▀▀▀▄    ▄▀▀▀▀▄█  ▀▀█▀▀▀▀▀█▀▀   ▄▀▀▀▀▄    ▄▀▀
   ▀█▄       ▄█▀   █     █     █  █      █    █     █    █▄▄▄▄▄▄█  █
     ▀█▄   ▄█▀     █     █     █  █      █    █     █    █         █
  ▄██▀▀█   █▀▀██▄  █     █     █   ▀▄▄▄▄▀█    ▀▄▄   ▀▄▄   ▀▄▄▄▄▀   █
 ▀▀    █   █    ▀▀                                                                                                                                                                                                                                         
`

// newVersionCmd creates the `matter version` subcommand.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			fmt.Fprint(w, matterLogo)
			fmt.Fprintf(w, "%s %s\n", output.Bold("matter"), output.Success(version))
			fmt.Fprintf(w, "  %s  %s\n", output.Label("commit:"), output.Value(commit))
			fmt.Fprintf(w, "  %s   %s\n", output.Label("built:"), output.Value(date))
			fmt.Fprintf(w, "  %s      %s\n", output.Label("go:"), output.Value(runtime.Version()))
			fmt.Fprintf(w, "  %s %s\n", output.Label("os/arch:"), output.Value(runtime.GOOS+"/"+runtime.GOARCH))
			return nil
		},
	}
}
