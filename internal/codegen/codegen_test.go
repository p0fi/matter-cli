// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package codegen

import (
	"strings"
	"testing"
)

const onOffXML = `<?xml version="1.0"?>
<cluster xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" id="0x0006" name="On/Off Cluster" revision="7">
  <clusterIds>
    <clusterId id="0x0006" name="On/Off"/>
  </clusterIds>
  <features>
    <feature bit="0" code="LT" name="Lighting" summary="Behavior that supports lighting applications."/>
    <feature bit="1" code="DF" name="DeadFrontBehavior" summary="Device has Dead Front behavior"/>
    <feature bit="2" code="OFFONLY" name="OffOnly" summary="Device supports the OffOnly feature"/>
  </features>
  <dataTypes>
    <enum name="StartUpOnOffEnum">
      <item value="0" name="Off"/>
      <item value="1" name="On"/>
      <item value="2" name="Toggle"/>
    </enum>
    <bitmap name="OnOffControlBitmap">
      <bitfield name="AcceptOnlyWhenOn" bit="0"/>
    </bitmap>
  </dataTypes>
  <attributes>
    <attribute id="0x0000" name="OnOff" type="bool">
      <access read="true" readPrivilege="view"/>
    </attribute>
    <attribute id="0x4000" name="GlobalSceneControl" type="bool">
      <access read="true" readPrivilege="view"/>
    </attribute>
    <attribute id="0x4001" name="OnTime" type="uint16">
      <access read="true" write="true" readPrivilege="view" writePrivilege="operate"/>
    </attribute>
    <attribute id="0x4003" name="StartUpOnOff" type="StartUpOnOffEnum">
      <access read="true" write="true" readPrivilege="view" writePrivilege="manage"/>
      <quality nullable="true"/>
    </attribute>
  </attributes>
  <commands>
    <command id="0x00" name="Off" direction="commandToServer" response="Y">
      <access invokePrivilege="operate"/>
    </command>
    <command id="0x01" name="On" direction="commandToServer" response="Y">
      <access invokePrivilege="operate"/>
    </command>
    <command id="0x02" name="Toggle" direction="commandToServer" response="Y">
      <access invokePrivilege="operate"/>
    </command>
    <command id="0x42" name="OnWithTimedOff" direction="commandToServer" response="Y">
      <access invokePrivilege="operate"/>
      <field id="0" name="OnOffControl" type="OnOffControlBitmap"/>
      <field id="1" name="OnTime" type="uint16"/>
      <field id="2" name="OffWaitTime" type="uint16"/>
    </command>
  </commands>
</cluster>`

func TestParseOnOff(t *testing.T) {
	clusters, err := Parse([]byte(onOffXML))
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
	c := clusters[0]
	if c.ID != 0x0006 {
		t.Errorf("ID = 0x%04X, want 0x0006", c.ID)
	}
	if c.Name != "OnOff" {
		t.Errorf("Name = %q, want %q", c.Name, "OnOff")
	}
	if c.DisplayName != "On/Off" {
		t.Errorf("DisplayName = %q, want %q", c.DisplayName, "On/Off")
	}
	if c.PackageName != "onoff" {
		t.Errorf("PackageName = %q, want %q", c.PackageName, "onoff")
	}
	// Check features.
	if len(c.Features) != 3 {
		t.Fatalf("expected 3 features, got %d", len(c.Features))
	}
	if c.Features[0].Bit != 0 || c.Features[0].Code != "LT" || c.Features[0].Name != "Lighting" {
		t.Errorf("feature[0] = %+v, want bit=0 code=LT name=Lighting", c.Features[0])
	}
	if c.Features[2].Bit != 2 || c.Features[2].Code != "OFFONLY" {
		t.Errorf("feature[2] = %+v, want bit=2 code=OFFONLY", c.Features[2])
	}
	if len(c.Attributes) != 4 {
		t.Fatalf("expected 4 attributes, got %d", len(c.Attributes))
	}
	// Check StartUpOnOff attribute resolves named enum to "enum8".
	attr := c.Attributes[3]
	if attr.Name != "StartUpOnOff" {
		t.Errorf("attr[3].Name = %q, want StartUpOnOff", attr.Name)
	}
	if attr.Type != "enum8" {
		t.Errorf("attr[3].Type = %q, want enum8", attr.Type)
	}
	if !attr.Nullable {
		t.Error("StartUpOnOff should be nullable")
	}
	if !attr.Writable {
		t.Error("StartUpOnOff should be writable")
	}

	// Check commands.
	if len(c.Commands) != 4 {
		t.Fatalf("expected 4 commands, got %d", len(c.Commands))
	}
	off := c.Commands[0]
	if off.Name != "Off" || off.HasRequest || off.HasResponse {
		t.Errorf("Off command: got name=%q hasReq=%v hasResp=%v", off.Name, off.HasRequest, off.HasResponse)
	}
	timed := c.Commands[3]
	if timed.Name != "OnWithTimedOff" || !timed.HasRequest {
		t.Errorf("OnWithTimedOff: got name=%q hasReq=%v", timed.Name, timed.HasRequest)
	}
	if len(timed.Fields) != 3 {
		t.Fatalf("OnWithTimedOff fields: expected 3, got %d", len(timed.Fields))
	}
	// Field 0 references OnOffControlBitmap.
	f0 := timed.Fields[0]
	if f0.Type != "bitmap8" {
		t.Errorf("field 0 type = %q, want bitmap8", f0.Type)
	}
	if f0.GoType != "uint8" {
		t.Errorf("field 0 GoType = %q, want uint8", f0.GoType)
	}
}

func TestParseMultiClusterIDs(t *testing.T) {
	xml := `<?xml version="1.0"?>
<cluster xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" name="Concentration Measurement Clusters" revision="4">
  <clusterIds>
    <clusterId id="0x040C" name="Carbon Monoxide Concentration Measurement"/>
    <clusterId id="0x042A" name="PM2.5 Concentration Measurement"/>
  </clusterIds>
  <attributes>
    <attribute id="0x0000" name="MeasuredValue" type="single">
      <access read="true" readPrivilege="view"/>
      <quality nullable="true"/>
    </attribute>
  </attributes>
</cluster>`
	clusters, err := Parse([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}
	if clusters[0].ID != 0x040C {
		t.Errorf("cluster[0].ID = 0x%04X, want 0x040C", clusters[0].ID)
	}
	if clusters[0].Name != "CarbonMonoxideConcentrationMeasurement" {
		t.Errorf("cluster[0].Name = %q", clusters[0].Name)
	}
	if clusters[1].ID != 0x042A {
		t.Errorf("cluster[1].ID = 0x%04X, want 0x042A", clusters[1].ID)
	}
	if clusters[1].Name != "PM25ConcentrationMeasurement" {
		t.Errorf("cluster[1].Name = %q", clusters[1].Name)
	}
	// Both share the same attributes.
	if len(clusters[0].Attributes) != 1 || len(clusters[1].Attributes) != 1 {
		t.Error("expected both clusters to have 1 attribute")
	}
}

func TestGenerate(t *testing.T) {
	clusters, err := Parse([]byte(onOffXML))
	if err != nil {
		t.Fatal(err)
	}
	src, err := Generate(&clusters[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, "package onoff") {
		t.Error("expected 'package onoff' in output")
	}
	if !strings.Contains(s, "ID uint32 = 0x0006") {
		t.Error("expected cluster ID constant")
	}
	if !strings.Contains(s, `Name = "OnOff"`) {
		t.Error("expected Name constant")
	}
	if !strings.Contains(s, "AttrOnOff") || !strings.Contains(s, "0x0000") {
		t.Errorf("expected AttrOnOff constant in output:\n%s", s)
	}
	if !strings.Contains(s, "CmdOff") || !strings.Contains(s, "0x00") {
		t.Errorf("expected CmdOff constant in output:\n%s", s)
	}
	if !strings.Contains(s, "OnWithTimedOffRequest struct") {
		t.Error("expected OnWithTimedOffRequest struct")
	}
	if !strings.Contains(s, `tlv:"0,uint"`) {
		t.Error("expected TLV struct tags")
	}
	if !strings.Contains(s, "HasRequest: true") {
		t.Error("expected HasRequest: true")
	}
	if !strings.Contains(s, "DO NOT EDIT") {
		t.Error("expected generated file header")
	}
	// Check features.
	if !strings.Contains(s, "FeatureLighting") || !strings.Contains(s, "1 << 0") {
		t.Errorf("expected FeatureLighting constant in output:\n%s", s)
	}
	if !strings.Contains(s, "FeatureOffOnly") || !strings.Contains(s, "1 << 2") {
		t.Errorf("expected FeatureOffOnly constant in output:\n%s", s)
	}
	if !strings.Contains(s, `Code: "LT"`) {
		t.Errorf("expected feature registration with Code LT in output:\n%s", s)
	}
}

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"On/Off", "OnOff"},
		{"Fan Control", "FanControl"},
		{"PM2.5 Concentration Measurement", "PM25ConcentrationMeasurement"},
		{"Color Control", "ColorControl"},
		{"Door Lock", "DoorLock"},
		{"Basic Information", "BasicInformation"},
	}
	for _, tt := range tests {
		got := toPascalCase(tt.in)
		if got != tt.want {
			t.Errorf("toPascalCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResponseCommand(t *testing.T) {
	xml := `<?xml version="1.0"?>
<cluster id="0x0101" name="Door Lock Cluster">
  <clusterIds>
    <clusterId id="0x0101" name="Door Lock"/>
  </clusterIds>
  <commands>
    <command id="0x00" name="LockDoor" direction="commandToServer" response="Y">
      <field id="0" name="PINCode" type="octstr">
        <optionalConform/>
      </field>
    </command>
    <command id="0x22" name="GetCredentialStatus" direction="commandToServer" response="GetCredentialStatusResponse">
      <field id="0" name="Credential" type="uint16"/>
    </command>
  </commands>
</cluster>`
	clusters, err := Parse([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	c := clusters[0]
	if len(c.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(c.Commands))
	}
	if c.Commands[0].HasResponse {
		t.Error("LockDoor should not have HasResponse (response=Y)")
	}
	if !c.Commands[1].HasResponse {
		t.Error("GetCredentialStatus should have HasResponse")
	}
	// Check optional field.
	if !c.Commands[0].Fields[0].Optional {
		t.Error("PINCode field should be optional")
	}
}
