// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// matter is a pure Go Matter controller CLI with modern ergonomics.
package main

import (
	"fmt"
	"os"

	"github.com/p0fi/matter-cli/cli"
	"github.com/p0fi/matter-cli/cli/output"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", output.ErrorIcon(), output.Error(err.Error()))
		os.Exit(1)
	}
}
