// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"context"
	"fmt"
	"os"
	"strings"

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
	renderOpts := &d2svg.RenderOpts{
		Pad: &pad,
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

	// Node container label includes vendor/product info.
	nodeLabel := fmt.Sprintf("%s · ProductID: 0x%04X", vendordb.FormatVendorID(data.VendorID), data.ProductID)

	fmt.Fprintf(&sb, "%s: %q {\n", d2SafeKey(name), nodeLabel)
	fmt.Fprintf(&sb, "    grid-columns: 2\n")

	// Endpoints nested inside the node container.
	for _, ep := range data.Endpoints {
		epKey := fmt.Sprintf("ep%d", ep.ID)
		epLabel := fmt.Sprintf("Endpoint %d", ep.ID)
		if len(ep.DeviceTypes) > 0 {
			if dtName, ok := deviceTypeNames[ep.DeviceTypes[0].ID]; ok {
				epLabel += " · " + dtName
			}
		}

		if data.Level >= 2 && len(ep.Clusters) > 0 {
			// Endpoint container with 2-column cluster grid.
			fmt.Fprintf(&sb, "  %s: %q {\n", epKey, epLabel)
			fmt.Fprintf(&sb, "    grid-columns: 2\n")
			for _, cl := range ep.Clusters {
				clKey := fmt.Sprintf("cl%04x", cl.ID)
				clName := cl.Name
				if clName == "" {
					clName = fmt.Sprintf("0x%04X", cl.ID)
				}

				if data.Level >= 3 && len(cl.Attrs) > 0 {
					// Cluster with attribute list using class shape.
					fmt.Fprintf(&sb, "    %s: %q {\n", clKey, fmt.Sprintf("%s (0x%04X)", clName, cl.ID))
					fmt.Fprintf(&sb, "      shape: class\n")
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
						label = fmt.Sprintf("%s (0x%04X)", clName, cl.ID)
					}
					if cl.ListErr != "" {
						label += " " + cl.ListErr
					}
					fmt.Fprintf(&sb, "    %s: %q\n", clKey, label)
				}
			}
			fmt.Fprintf(&sb, "  }\n")
		} else {
			fmt.Fprintf(&sb, "  %s: %q\n", epKey, epLabel)
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
