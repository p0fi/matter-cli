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
				fmt.Fprintf(w, "%s%s\n", Dim(clPrefix), Muted(fmt.Sprintf("0x%04X", cl.ID)))
			} else {
				fmt.Fprintf(w, "%s%s %s\n", Dim(clPrefix), Info(name), Muted(fmt.Sprintf("(0x%04X)", cl.ID)))
			}
		}
		if !isLast {
			fmt.Fprintf(w, "%s\n", Dim(childPrefix[:len(childPrefix)-2]))
		}
	}
	return nil
}

// --- Rich tree types for the `tree` command ---

// TreeAttribute holds a single attribute entry in the device tree.
type TreeAttribute struct {
	ID    uint32
	Name  string // resolved display name, or "0x%04X" fallback
	Value string // empty unless level >= 4
	Err   string // non-empty if read failed (e.g. "<timeout>")
}

// TreeCluster holds a cluster entry with optional attribute data.
type TreeCluster struct {
	ID      uint32
	Name    string
	ListErr string          // non-empty if AttributeList read failed
	Attrs   []TreeAttribute // populated for level >= 3
}

// TreeEndpoint holds an endpoint entry with cluster data.
type TreeEndpoint struct {
	ID          uint16
	DeviceTypes []store.DeviceType
	Clusters    []TreeCluster
}

// TreeData is the full device tree data passed to FormatRichTree.
type TreeData struct {
	NodeID      uint64
	NodeName    string
	VendorID    uint16
	ProductID   uint16
	LastAddress string
	Endpoints   []TreeEndpoint
	Level       int
}

// FormatRichTree renders the device tree with depth controlled by data.Level:
//
//	1 – endpoints only
//	2 – endpoints + clusters
//	3 – endpoints + clusters + attribute names
//	4 – endpoints + clusters + attribute names + values
func FormatRichTree(w io.Writer, data *TreeData) error {
	name := data.NodeName
	if name == "" {
		name = "Unnamed"
	}
	fmt.Fprintf(w, "%s %s\n", Bold(name), Muted(fmt.Sprintf("(Node %d)", data.NodeID)))
	fmt.Fprintf(w, "  %s  %s\n", Label("Vendor:"), Accent(vendordb.FormatVendorID(data.VendorID)))
	fmt.Fprintf(w, "  %s %s\n", Label("Product:"), Accent(fmt.Sprintf("0x%04X", data.ProductID)))
	if data.LastAddress != "" {
		fmt.Fprintf(w, "  %s %s\n", Label("Address:"), Value(data.LastAddress))
	}
	fmt.Fprintln(w)

	if len(data.Endpoints) == 0 {
		fmt.Fprintln(w, Muted("  No endpoints"))
		return nil
	}

	fmt.Fprintln(w, Header("  Endpoints:"))
	for i, ep := range data.Endpoints {
		isLastEP := i == len(data.Endpoints)-1
		epPrefix := "  ├── "
		epChild := "  │   "
		if isLastEP {
			epPrefix = "  └── "
			epChild = "      "
		}

		dtName := ""
		if len(ep.DeviceTypes) > 0 {
			dtName = Muted(fmt.Sprintf(" (0x%04X)", ep.DeviceTypes[0].ID))
		}
		fmt.Fprintf(w, "%s%s%s\n", Dim(epPrefix), Bold(fmt.Sprintf("Endpoint %d", ep.ID)), dtName)

		if data.Level >= 2 {
			for j, cl := range ep.Clusters {
				isLastCL := j == len(ep.Clusters)-1
				clPrefix := epChild + "├── "
				clChild := epChild + "│   "
				if isLastCL {
					clPrefix = epChild + "└── "
					clChild = epChild + "    "
				}

				clName := cl.Name
				var clLine string
				if clName == "" {
					// Unknown cluster — show only the hex ID to avoid redundancy.
					clLine = fmt.Sprintf("%s%s", Dim(clPrefix), Muted(fmt.Sprintf("0x%04X", cl.ID)))
				} else {
					clLine = fmt.Sprintf("%s%s %s", Dim(clPrefix), Info(clName), Muted(fmt.Sprintf("(0x%04X)", cl.ID)))
				}
				if cl.ListErr != "" {
					clLine += " " + Muted(cl.ListErr)
				}
				fmt.Fprintln(w, clLine)

				if data.Level >= 3 && len(cl.Attrs) > 0 {
					for k, attr := range cl.Attrs {
						isLastAttr := k == len(cl.Attrs)-1
						atPrefix := clChild + "├── "
						if isLastAttr {
							atPrefix = clChild + "└── "
						}

						attrLine := attr.Name
						if attr.Err != "" {
							attrLine += " " + Muted(attr.Err)
						} else if data.Level >= 4 && attr.Value != "" {
							attrLine += " = " + Value(attr.Value)
						}
						fmt.Fprintf(w, "%s%s %s\n", Dim(atPrefix), Info(attrLine), Muted(fmt.Sprintf("(0x%04X)", attr.ID)))
					}
				}
			}
		}

		if !isLastEP {
			fmt.Fprintf(w, "%s\n", Dim(epChild[:len(epChild)-2]))
		}
	}
	return nil
}
