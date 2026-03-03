// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Command codegen generates Go cluster packages from Alchemy Matter XML files.
//
// Usage:
//
//	go run ./cmd/codegen -xml xml/clusters -out internal/clusters
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/p0fi/matter-cli/internal/codegen"
)

func main() {
	xmlDir := flag.String("xml", "xml/clusters", "directory containing Alchemy cluster XML files")
	outDir := flag.String("out", "internal/clusters", "output directory for generated Go packages")
	module := flag.String("module", "github.com/p0fi/matter-cli", "Go module path")
	allImports := flag.Bool("all-imports", true, "generate all/all.go import file")
	flag.Parse()

	fmt.Printf("codegen: parsing XML from %s\n", *xmlDir)
	clusters, err := codegen.GenerateAll(*xmlDir, *outDir, *module)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("codegen: generated %d cluster packages in %s\n", len(clusters), *outDir)

	if *allImports {
		if err := codegen.GenerateAllImports(clusters, *module, *outDir); err != nil {
			fmt.Fprintf(os.Stderr, "error generating all imports: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("codegen: wrote all/all.go\n")
	}
}
