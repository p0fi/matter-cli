// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"io"

	"github.com/p0fi/matter-cli/internal/store"
	"github.com/p0fi/matter-cli/internal/vendordb"
)

// FormatTree writes a tree-style representation of a node to w,
// showing endpoints, clusters, and device types.
func FormatTree(w io.Writer, node *store.Node) error {
	name := node.Name
	if name == "" {
		name = "Unnamed"
	}
	fmt.Fprintf(w, "%s %s\n", Bold(name), Muted(fmt.Sprintf("(Node %d)", node.ID)))
	fmt.Fprintf(w, "  %s  %s\n", Label("Vendor:"), Accent(vendordb.FormatVendorID(node.VendorID)))
	fmt.Fprintf(w, "  %s %s\n", Label("Product:"), Accent(fmt.Sprintf("0x%04X", node.ProductID)))
	if node.LastAddress != "" {
		fmt.Fprintf(w, "  %s %s\n", Label("Address:"), Value(node.LastAddress))
	}
	fmt.Fprintln(w)

	if len(node.Endpoints) == 0 {
		fmt.Fprintln(w, Muted("  No endpoints"))
		return nil
	}

	fmt.Fprintln(w, Header("  Endpoints:"))
	for i, ep := range node.Endpoints {
		isLast := i == len(node.Endpoints)-1
		prefix := "  ├── "
		childPrefix := "  │   "
		if isLast {
			prefix = "  └── "
			childPrefix = "      "
		}

		dtName := ""
		if len(ep.DeviceTypes) > 0 {
			dtName = Muted(fmt.Sprintf(" (0x%04X)", ep.DeviceTypes[0].ID))
		}
		fmt.Fprintf(w, "%s%s%s\n", Dim(prefix), Bold(fmt.Sprintf("Endpoint %d", ep.ID)), dtName)

		for j, cl := range ep.Clusters {
			isLastCluster := j == len(ep.Clusters)-1
			clPrefix := childPrefix + "├── "
			if isLastCluster {
				clPrefix = childPrefix + "└── "
			}
			name := cl.Name
			if name == "" {
				name = fmt.Sprintf("Cluster 0x%04X", cl.ID)
			}
			fmt.Fprintf(w, "%s%s %s\n", Dim(clPrefix), Info(name), Muted(fmt.Sprintf("(0x%04X)", cl.ID)))
		}
		if !isLast {
			fmt.Fprintf(w, "%s\n", Dim(childPrefix[:len(childPrefix)-2]))
		}
	}
	return nil
}
