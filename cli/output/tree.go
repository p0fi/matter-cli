// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/p0fi/matter-cli/internal/store"
	"github.com/p0fi/matter-cli/internal/vendordb"
)

const (
	epGridGap    = 4 // spaces between endpoint columns in multi-column layout
	epGridMargin = 2 // left margin spaces for the grid
)

// deviceTypeNames maps well-known Matter Device Library type IDs to their
// human-readable names.
var deviceTypeNames = map[uint32]string{
	// Infrastructure
	0x0016: "Root Node",
	0x0019: "Bridged Node",
	0x000E: "Aggregator",
	0x000F: "Power Source",
	// Lighting
	0x0100: "On/Off Light",
	0x0101: "Dimmable Light",
	0x0102: "Color Temperature Light",
	0x0103: "Extended Color Light",
	// Plugs & outlets
	0x0104: "On/Off Plug-in Unit",
	0x0105: "Dimmable Plug-in Unit",
	// Switches & controls
	0x010A: "On/Off Light Switch",
	0x010B: "Dimmer Switch",
	0x010C: "Color Dimmer Switch",
	0x010D: "Generic Switch",
	0x0840: "Control Bridge",
	0x0050: "On/Off Sensor",
	// Window coverings
	0x0202: "Window Covering",
	0x0203: "Window Covering Controller",
	// HVAC & fans
	0x0300: "Heating/Cooling Unit",
	0x0301: "Thermostat",
	0x002B: "Fan",
	// Air quality (Matter 1.2+)
	0x002C: "Air Quality Sensor",
	0x002D: "Air Purifier",
	// Access control
	0x000A: "Door Lock",
	0x000B: "Door Lock Controller",
	// Sensors
	0x0015: "Contact Sensor",
	0x0106: "Light Sensor",
	0x0107: "Occupancy Sensor",
	0x0302: "Temperature Sensor",
	0x0303: "Pump",
	0x0304: "Pump Controller",
	0x0305: "Pressure Sensor",
	0x0306: "Flow Sensor",
	0x0307: "Humidity Sensor",
	// Appliances
	0x0070: "Refrigerator",
	0x0071: "Temperature Controlled Cabinet",
	0x0072: "Room Air Conditioner",
	0x0073: "Laundry Washer",
	0x0074: "Robotic Vacuum Cleaner",
	0x0075: "Dishwasher",
	0x0076: "Smoke/CO Alarm",
}

// epDeviceType returns the muted device-type annotation for an endpoint header,
// e.g. "Root Node (0x0016)" or just "(0x0016)" when the type is unknown.
func epDeviceType(dt store.DeviceType) string {
	if name, ok := deviceTypeNames[dt.ID]; ok {
		return Muted(fmt.Sprintf("%s (0x%04X)", name, dt.ID))
	}
	return Muted(fmt.Sprintf("(0x%04X)", dt.ID))
}

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

	fmt.Fprintln(w, Header("  Node"))
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
	Side    string          // "server" or "client"
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
//
// On wide terminals endpoints are shown side-by-side: a branch bar fans out
// from the node and vertical drop lines connect down to each endpoint column.
// On narrow terminals or pipes the classic top-down tree is used instead.
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

	fmt.Fprintln(w, Header("  Node"))

	// Try multi-column grid layout on wide terminals.
	gridRendered := 0
	var blocks [][]string
	if termW := TermWidth(); termW > 0 && len(data.Endpoints) >= 2 {
		blocks = make([][]string, len(data.Endpoints))
		for i := range data.Endpoints {
			blocks[i] = epBlock(&data.Endpoints[i], data)
		}

		// Determine how many endpoints fit in one row.
		// Account for the 4-char tree prefix added to each block.
		const treePfxWidth = 4
		blockWidths := make([]int, len(blocks))
		for i, b := range blocks {
			for _, line := range b {
				if vw := visWidth(line); vw > blockWidths[i] {
					blockWidths[i] = vw
				}
			}
			blockWidths[i] += treePfxWidth
		}
		available := termW - epGridMargin
		usedW := 0
		for _, bw := range blockWidths {
			needed := bw
			if gridRendered > 0 {
				needed += epGridGap
			}
			if usedW+needed > available {
				break
			}
			usedW += needed
			gridRendered++
		}
		if gridRendered < 2 {
			gridRendered = 0
		}

		if gridRendered > 0 {
			// Prepend tree connectors. Only the first column gets a
			// spine (│) when there are more endpoints below the grid.
			moreBelow := gridRendered < len(data.Endpoints)
			for i := range blocks {
				useSpine := i == 0 && moreBelow
				hdrPfx := Dim("└── ")
				chdPfx := Dim("    ")
				if useSpine {
					hdrPfx = Dim("├── ")
					chdPfx = Dim("│   ")
				}
				for j := range blocks[i] {
					if j == 0 {
						blocks[i][j] = hdrPfx + blocks[i][j]
					} else {
						blocks[i][j] = chdPfx + blocks[i][j]
					}
				}
			}

			renderGridRow(w, blocks[:gridRendered], blockWidths[:gridRendered], epGridGap)
			if gridRendered == len(data.Endpoints) {
				return nil
			}
		}
	}

	// Render remaining endpoints (or all, if grid didn't fit any) as
	// single-column tree entries with ├──/└── connectors.
	if gridRendered > 0 {
		fmt.Fprintf(w, "%s\n", Dim("  │"))
	}
	for i := gridRendered; i < len(data.Endpoints); i++ {
		ep := data.Endpoints[i]
		isLastEP := i == len(data.Endpoints)-1
		epPrefix := "  ├── "
		epChild := "  │   "
		if isLastEP {
			epPrefix = "  └── "
			epChild = "      "
		}

		dtName := ""
		if len(ep.DeviceTypes) > 0 {
			dtName = " " + epDeviceType(ep.DeviceTypes[0])
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
					clLine = fmt.Sprintf("%s%s", Dim(clPrefix), Muted(fmt.Sprintf("0x%04X", cl.ID)))
				} else {
					clLine = fmt.Sprintf("%s%s %s", Dim(clPrefix), Header(clName), Muted(fmt.Sprintf("(0x%04X)", cl.ID)))
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

// epBlock renders a single endpoint into a slice of display lines for the
// multi-column grid. Lines have no outer tree connector — the branch bar
// printed above the grid provides the visual connection to the node.
func epBlock(ep *TreeEndpoint, data *TreeData) []string {
	dtName := ""
	if len(ep.DeviceTypes) > 0 {
		dtName = " " + epDeviceType(ep.DeviceTypes[0])
	}
	lines := []string{fmt.Sprintf("%s%s", Bold(fmt.Sprintf("Endpoint %d", ep.ID)), dtName)}

	if data.Level < 2 {
		return lines
	}

	for j, cl := range ep.Clusters {
		isLastCL := j == len(ep.Clusters)-1
		clPfx := "├── "
		clChd := "│   "
		if isLastCL {
			clPfx = "└── "
			clChd = "    "
		}

		clName := cl.Name
		var ln string
		if clName == "" {
			ln = fmt.Sprintf("%s%s", Dim(clPfx), Muted(fmt.Sprintf("0x%04X", cl.ID)))
		} else {
			ln = fmt.Sprintf("%s%s %s", Dim(clPfx), Header(clName), Muted(fmt.Sprintf("(0x%04X)", cl.ID)))
		}
		if cl.ListErr != "" {
			ln += " " + Muted(cl.ListErr)
		}
		lines = append(lines, ln)

		if data.Level >= 3 {
			for k, attr := range cl.Attrs {
				isLastAttr := k == len(cl.Attrs)-1
				atPfx := clChd + "├── "
				if isLastAttr {
					atPfx = clChd + "└── "
				}
				attrLine := attr.Name
				if attr.Err != "" {
					attrLine += " " + Muted(attr.Err)
				} else if data.Level >= 4 && attr.Value != "" {
					attrLine += " = " + Value(attr.Value)
				}
				lines = append(lines, fmt.Sprintf("%s%s %s", Dim(atPfx), Info(attrLine), Muted(fmt.Sprintf("(0x%04X)", attr.ID))))
			}
		}
	}
	return lines
}

// buildBranchBar returns the horizontal branch bar that fans out from the node
// to endpoint columns, e.g. "├─────────────┬─────────────┐".
// colWidths holds per-column visible widths; gap is the space between columns.
func buildBranchBar(colWidths []int, gap int) string {
	var sb strings.Builder
	sb.WriteString("├")
	for i := 1; i < len(colWidths); i++ {
		sb.WriteString(strings.Repeat("─", colWidths[i-1]+gap-1))
		if i < len(colWidths)-1 {
			sb.WriteString("┬")
		} else {
			sb.WriteString("┐")
		}
	}
	return Dim(sb.String())
}

// buildVertDrop returns the vertical-drop row that connects each connector
// in the branch bar down to the endpoint header below it,
// e.g. "│             │             │".
func buildVertDrop(colWidths []int, gap int) string {
	var sb strings.Builder
	for i := 0; i < len(colWidths); i++ {
		sb.WriteString("│")
		if i < len(colWidths)-1 {
			sb.WriteString(strings.Repeat(" ", colWidths[i]+gap-1))
		}
	}
	return Dim(sb.String())
}

// renderGridRow renders a single row of endpoint blocks side by side using
// the given per-column widths.
func renderGridRow(w io.Writer, blocks [][]string, colWidths []int, gap int) {
	margin := strings.Repeat(" ", epGridMargin)

	fmt.Fprintf(w, "%s%s\n", margin, buildBranchBar(colWidths, gap))
	fmt.Fprintf(w, "%s%s\n", margin, buildVertDrop(colWidths, gap))

	maxH := 0
	for _, b := range blocks {
		if len(b) > maxH {
			maxH = len(b)
		}
	}

	for lineIdx := 0; lineIdx < maxH; lineIdx++ {
		// Find the rightmost column that still has content on this line
		// so we don't emit trailing whitespace for exhausted columns.
		lastContent := 0
		for ci, b := range blocks {
			if lineIdx < len(b) {
				lastContent = ci
			}
		}

		fmt.Fprint(w, margin)
		for ci, b := range blocks {
			s := ""
			if lineIdx < len(b) {
				s = b[lineIdx]
			}
			isLast := ci >= lastContent
			if s == "" {
				if !isLast {
					fmt.Fprint(w, strings.Repeat(" ", colWidths[ci]+gap))
				}
			} else if !isLast {
				pad := colWidths[ci] - visWidth(s)
				if pad < 0 {
					pad = 0
				}
				fmt.Fprint(w, s)
				fmt.Fprint(w, strings.Repeat(" ", pad+gap))
			} else {
				fmt.Fprint(w, s)
			}
		}
		fmt.Fprintln(w)
	}
}
