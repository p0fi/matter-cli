// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/p0fi/matter-cli/internal/clusters/basicinformation"
	"github.com/p0fi/matter-cli/internal/vendordb"
	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
)

// RenderTreeSVG converts TreeData into a D2 diagram and renders it as SVG to
// the given file path.
func RenderTreeSVG(data *TreeData, filename string) error {
	script := buildD2Script(data)

	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return fmt.Errorf("creating text ruler: %w", err)
	}

	layoutResolver := func(engine string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}

	pad := int64(d2svg.DEFAULT_PADDING)
	themeID := int64(1) // Neutral theme
	renderOpts := &d2svg.RenderOpts{
		Pad:     &pad,
		ThemeID: &themeID,
	}

	compileOpts := &d2lib.CompileOptions{
		LayoutResolver: layoutResolver,
		Ruler:          ruler,
	}

	ctx := log.WithDefault(context.Background())
	diagram, _, err := d2lib.Compile(ctx, script, compileOpts, renderOpts)
	if err != nil {
		return fmt.Errorf("compiling D2 diagram: %w", err)
	}

	out, err := d2svg.Render(diagram, renderOpts)
	if err != nil {
		return fmt.Errorf("rendering SVG: %w", err)
	}

	return os.WriteFile(filename, out, 0644)
}

// utilityClusterIDs contains cluster IDs that are infrastructure/utility
// clusters (present on most endpoints) rather than application-specific.
var utilityClusterIDs = map[uint32]bool{
	0x001D: true, // Descriptor
	0x001E: true, // Binding
	0x001F: true, // AccessControl
	0x0028: true, // BasicInformation
	0x0030: true, // GeneralCommissioning
	0x0031: true, // NetworkCommissioning
	0x0033: true, // GeneralDiagnostics
	0x0034: true, // SoftwareDiagnostics
	0x0035: true, // ThreadNetworkDiagnostics
	0x0036: true, // WiFiNetworkDiagnostics
	0x0037: true, // EthernetNetworkDiagnostics
	0x003C: true, // AdministratorCommissioning
	0x003E: true, // OperationalCredentials
	0x003F: true, // GroupKeyManagement
	0x0040: true, // FixedLabel
	0x0041: true, // UserLabel
}

// epCategoryColor returns a hex fill color for an endpoint based on its
// primary device type, giving a subtle visual cue about its role.
func epCategoryColor(ep TreeEndpoint) string {
	if len(ep.DeviceTypes) == 0 {
		return "#F0F0F0"
	}
	dtID := ep.DeviceTypes[0].ID
	switch {
	case dtID == 0x0016: // Root Node
		return "#E8EAF6" // light indigo
	case dtID >= 0x0100 && dtID <= 0x0105: // Lighting & plugs
		return "#FFF8E1" // light amber
	case dtID >= 0x010A && dtID <= 0x010D, dtID == 0x0840, dtID == 0x0050: // Switches & controls
		return "#E8F5E9" // light green
	case dtID == 0x0202 || dtID == 0x0203: // Window coverings
		return "#E3F2FD" // light blue
	case dtID >= 0x0300 && dtID <= 0x0307, dtID == 0x002B: // HVAC, fans, sensors
		return "#FFF3E0" // light orange
	case dtID == 0x000A || dtID == 0x000B: // Door lock
		return "#FCE4EC" // light pink
	default:
		return "#F5F5F5" // light grey
	}
}

// clusterSideTag returns a short side indicator for cluster labels.
func clusterSideTag(side string) string {
	switch side {
	case "server":
		return " [S]"
	case "client":
		return " [C]"
	default:
		return ""
	}
}

// buildD2Script generates a D2 script from TreeData. The node is an outer
// container holding endpoint containers. Each endpoint uses a 2-column grid
// for its clusters.
func buildD2Script(data *TreeData) string {
	var sb strings.Builder

	// Node name for the container key.
	name := data.NodeName
	if name == "" {
		name = "Unnamed"
	}

	// Node container label includes vendor/product info and optional device details.
	nodeLabel := fmt.Sprintf("%s · ProductID: 0x%04X", vendordb.FormatVendorID(data.VendorID), data.ProductID)
	var details []string
	if sv := basicinformation.FormatSpecVersion(data.SpecificationVersion); sv != "" {
		details = append(details, "Matter "+sv)
	}
	if data.SoftwareVersion != 0 {
		details = append(details, fmt.Sprintf("FW %d", data.SoftwareVersion))
	}
	if data.SerialNumber != "" {
		details = append(details, "SN "+data.SerialNumber)
	}
	if len(details) > 0 {
		nodeLabel += "\n" + strings.Join(details, " · ")
	}

	fmt.Fprintf(&sb, "%s: %q {\n", d2SafeKey(name), nodeLabel)
	fmt.Fprintf(&sb, "    grid-columns: 2\n")
	fmt.Fprintf(&sb, "    style.stroke-width: 3\n")
	fmt.Fprintf(&sb, "    style.fill: %q\n", "#FAFAFA")

	// Endpoints nested inside the node container.
	for _, ep := range data.Endpoints {
		epKey := fmt.Sprintf("ep%d", ep.ID)
		epLabel := fmt.Sprintf("Endpoint %d", ep.ID)
		if len(ep.DeviceTypes) > 0 {
			if dtName, ok := deviceTypeNames[ep.DeviceTypes[0].ID]; ok {
				epLabel += " · " + dtName
			}
		}

		epFill := epCategoryColor(ep)

		if data.Level >= 2 && len(ep.Clusters) > 0 {
			// Endpoint container with 2-column cluster grid.
			fmt.Fprintf(&sb, "  %s: %q {\n", epKey, epLabel)
			fmt.Fprintf(&sb, "    grid-columns: 2\n")
			fmt.Fprintf(&sb, "    style.fill: %q\n", epFill)
			for _, cl := range ep.Clusters {
				clKey := fmt.Sprintf("cl%04x", cl.ID)
				clName := cl.Name
				if clName == "" {
					clName = fmt.Sprintf("0x%04X", cl.ID)
				}

				sideTag := clusterSideTag(cl.Side)

				if data.Level >= 3 && len(cl.Attrs) > 0 {
					// Cluster with attribute list using class shape.
					fmt.Fprintf(&sb, "    %s: %q {\n", clKey, fmt.Sprintf("%s (0x%04X)%s", clName, cl.ID, sideTag))
					fmt.Fprintf(&sb, "      shape: class\n")
					if utilityClusterIDs[cl.ID] {
						fmt.Fprintf(&sb, "      style.opacity: 0.7\n")
					}
					for _, attr := range cl.Attrs {
						attrKey := d2SafeKey(attr.Name)
						switch {
						case data.Level >= 4 && attr.Value != "":
							fmt.Fprintf(&sb, "      %s: %q\n", attrKey, attr.Value)
						case attr.Err != "":
							fmt.Fprintf(&sb, "      %s: %q\n", attrKey, attr.Err)
						default:
							fmt.Fprintf(&sb, "      %s\n", attrKey)
						}
					}
					fmt.Fprintf(&sb, "    }\n")
				} else {
					label := clName
					if cl.Name != "" {
						label = fmt.Sprintf("%s (0x%04X)%s", clName, cl.ID, sideTag)
					}
					if cl.ListErr != "" {
						label += " " + cl.ListErr
					}
					fmt.Fprintf(&sb, "    %s: %q", clKey, label)
					if utilityClusterIDs[cl.ID] {
						fmt.Fprintf(&sb, " {\n      style.opacity: 0.7\n    }\n")
					} else {
						fmt.Fprintf(&sb, "\n")
					}
				}
			}
			fmt.Fprintf(&sb, "  }\n")
		} else {
			fmt.Fprintf(&sb, "  %s: %q {\n", epKey, epLabel)
			fmt.Fprintf(&sb, "    style.fill: %q\n", epFill)
			fmt.Fprintf(&sb, "  }\n")
		}
	}

	fmt.Fprintf(&sb, "}\n")
	return sb.String()
}

// d2SafeKey wraps a string in double quotes if it contains characters that are
// not safe as bare D2 identifiers.
func d2SafeKey(s string) string {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-') {
			return fmt.Sprintf("%q", s)
		}
	}
	return s
}
