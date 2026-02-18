// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"errors"
	"testing"
	"time"
)

// storeTestSuite runs a comprehensive set of tests against any Store
// implementation. Call it from each implementation's test file with a
// freshly-created store.
func storeTestSuite(t *testing.T, s Store) {
	t.Helper()

	t.Run("Fabric_CRUD", func(t *testing.T) {
		fabric := &Fabric{
			ID:            1,
			Label:         "test-fabric",
			RootCertPEM:   "root-pem",
			ICACertPEM:    "ica-pem",
			PrivateKeyPEM: "key-pem",
			VendorID:      0xFFF1,
			FabricIndex:   1,
			CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}

		// Save
		if err := s.SaveFabric(fabric); err != nil {
			t.Fatalf("SaveFabric: %v", err)
		}

		// Get
		got, err := s.GetFabric(1)
		if err != nil {
			t.Fatalf("GetFabric: %v", err)
		}
		if got.Label != "test-fabric" {
			t.Errorf("Label = %q, want %q", got.Label, "test-fabric")
		}
		if got.VendorID != 0xFFF1 {
			t.Errorf("VendorID = %d, want %d", got.VendorID, 0xFFF1)
		}
		if !got.CreatedAt.Equal(fabric.CreatedAt) {
			t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, fabric.CreatedAt)
		}

		// Update
		fabric.Label = "updated"
		if err := s.SaveFabric(fabric); err != nil {
			t.Fatalf("SaveFabric (update): %v", err)
		}
		got, err = s.GetFabric(1)
		if err != nil {
			t.Fatalf("GetFabric after update: %v", err)
		}
		if got.Label != "updated" {
			t.Errorf("Label after update = %q, want %q", got.Label, "updated")
		}

		// List
		fabric2 := &Fabric{ID: 2, Label: "second"}
		if err := s.SaveFabric(fabric2); err != nil {
			t.Fatalf("SaveFabric second: %v", err)
		}
		fabrics, err := s.ListFabrics()
		if err != nil {
			t.Fatalf("ListFabrics: %v", err)
		}
		if len(fabrics) != 2 {
			t.Fatalf("ListFabrics len = %d, want 2", len(fabrics))
		}

		// Delete
		if err := s.DeleteFabric(2); err != nil {
			t.Fatalf("DeleteFabric: %v", err)
		}
		fabrics, err = s.ListFabrics()
		if err != nil {
			t.Fatalf("ListFabrics after delete: %v", err)
		}
		if len(fabrics) != 1 {
			t.Fatalf("ListFabrics len after delete = %d, want 1", len(fabrics))
		}
	})

	t.Run("Fabric_NotFound", func(t *testing.T) {
		_, err := s.GetFabric(9999)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("GetFabric non-existent: got %v, want ErrNotFound", err)
		}
		err = s.DeleteFabric(9999)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteFabric non-existent: got %v, want ErrNotFound", err)
		}
	})

	t.Run("Node_CRUD", func(t *testing.T) {
		// Ensure parent fabric exists.
		if err := s.SaveFabric(&Fabric{ID: 10, Label: "node-test-fabric"}); err != nil {
			t.Fatalf("SaveFabric: %v", err)
		}

		node := &Node{
			ID:        100,
			Name:      "light-bulb",
			VendorID:  0xFFF1,
			ProductID: 0x8001,
			Endpoints: []Endpoint{
				{
					ID:          1,
					DeviceTypes: []DeviceType{{ID: 256, Revision: 1}},
					Clusters:    []ClusterRef{{ID: 6, Name: "OnOff", Side: "server"}},
				},
			},
			LastAddress: "192.168.1.42:5540",
			LastSeen:    time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC),
		}

		// Save
		if err := s.SaveNode(10, node); err != nil {
			t.Fatalf("SaveNode: %v", err)
		}

		// Get
		got, err := s.GetNode(10, 100)
		if err != nil {
			t.Fatalf("GetNode: %v", err)
		}
		if got.Name != "light-bulb" {
			t.Errorf("Name = %q, want %q", got.Name, "light-bulb")
		}
		if got.FabricID != 10 {
			t.Errorf("FabricID = %d, want 10", got.FabricID)
		}
		if len(got.Endpoints) != 1 {
			t.Fatalf("len(Endpoints) = %d, want 1", len(got.Endpoints))
		}
		if got.Endpoints[0].Clusters[0].Name != "OnOff" {
			t.Errorf("Cluster name = %q, want %q", got.Endpoints[0].Clusters[0].Name, "OnOff")
		}

		// List
		node2 := &Node{ID: 101, Name: "thermostat"}
		if err := s.SaveNode(10, node2); err != nil {
			t.Fatalf("SaveNode second: %v", err)
		}
		nodes, err := s.ListNodes(10)
		if err != nil {
			t.Fatalf("ListNodes: %v", err)
		}
		if len(nodes) != 2 {
			t.Fatalf("ListNodes len = %d, want 2", len(nodes))
		}

		// Delete
		if err := s.DeleteNode(10, 101); err != nil {
			t.Fatalf("DeleteNode: %v", err)
		}
		nodes, err = s.ListNodes(10)
		if err != nil {
			t.Fatalf("ListNodes after delete: %v", err)
		}
		if len(nodes) != 1 {
			t.Fatalf("ListNodes len after delete = %d, want 1", len(nodes))
		}
	})

	t.Run("Node_FabricNotFound", func(t *testing.T) {
		err := s.SaveNode(9999, &Node{ID: 1})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("SaveNode unknown fabric: got %v, want ErrNotFound", err)
		}
		_, err = s.ListNodes(9999)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("ListNodes unknown fabric: got %v, want ErrNotFound", err)
		}
	})

	t.Run("Node_NotFound", func(t *testing.T) {
		_, err := s.GetNode(10, 9999)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("GetNode non-existent: got %v, want ErrNotFound", err)
		}
		err = s.DeleteNode(10, 9999)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteNode non-existent: got %v, want ErrNotFound", err)
		}
	})

	t.Run("DeleteFabric_CascadesNodes", func(t *testing.T) {
		if err := s.SaveFabric(&Fabric{ID: 20, Label: "cascade"}); err != nil {
			t.Fatalf("SaveFabric: %v", err)
		}
		if err := s.SaveNode(20, &Node{ID: 200, Name: "to-be-deleted"}); err != nil {
			t.Fatalf("SaveNode: %v", err)
		}
		if err := s.DeleteFabric(20); err != nil {
			t.Fatalf("DeleteFabric: %v", err)
		}
		_, err := s.GetNode(20, 200)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("GetNode after fabric delete: got %v, want ErrNotFound", err)
		}
	})

	t.Run("ResumptionInfo_CRUD", func(t *testing.T) {
		info := &ResumptionInfo{
			PeerNodeID:      42,
			ResumptionID:    []byte{1, 2, 3, 4},
			SharedSecret:    []byte{5, 6, 7, 8},
			CASESessionParams: []byte{9, 10},
		}

		if err := s.SaveResumptionInfo(info); err != nil {
			t.Fatalf("SaveResumptionInfo: %v", err)
		}

		got, err := s.GetResumptionInfo(42)
		if err != nil {
			t.Fatalf("GetResumptionInfo: %v", err)
		}
		if got.PeerNodeID != 42 {
			t.Errorf("PeerNodeID = %d, want 42", got.PeerNodeID)
		}
		if len(got.ResumptionID) != 4 || got.ResumptionID[0] != 1 {
			t.Errorf("ResumptionID = %v, want [1 2 3 4]", got.ResumptionID)
		}
		if len(got.SharedSecret) != 4 || got.SharedSecret[0] != 5 {
			t.Errorf("SharedSecret = %v, want [5 6 7 8]", got.SharedSecret)
		}
	})

	t.Run("ResumptionInfo_NotFound", func(t *testing.T) {
		_, err := s.GetResumptionInfo(9999)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("GetResumptionInfo non-existent: got %v, want ErrNotFound", err)
		}
	})

	t.Run("KV_SetGet", func(t *testing.T) {
		if err := s.Set("some-key", []byte("some-value")); err != nil {
			t.Fatalf("Set: %v", err)
		}
		got, err := s.Get("some-key")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(got) != "some-value" {
			t.Errorf("Get = %q, want %q", string(got), "some-value")
		}

		// Overwrite
		if err := s.Set("some-key", []byte("updated")); err != nil {
			t.Fatalf("Set overwrite: %v", err)
		}
		got, err = s.Get("some-key")
		if err != nil {
			t.Fatalf("Get after overwrite: %v", err)
		}
		if string(got) != "updated" {
			t.Errorf("Get after overwrite = %q, want %q", string(got), "updated")
		}
	})

	t.Run("KV_NotFound", func(t *testing.T) {
		_, err := s.Get("no-such-key")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("Get non-existent: got %v, want ErrNotFound", err)
		}
	})

	t.Run("NilInputs", func(t *testing.T) {
		if err := s.SaveFabric(nil); err == nil {
			t.Error("SaveFabric(nil) should return error")
		}
		if err := s.SaveNode(10, nil); err == nil {
			t.Error("SaveNode(nil) should return error")
		}
		if err := s.SaveResumptionInfo(nil); err == nil {
			t.Error("SaveResumptionInfo(nil) should return error")
		}
	})
}
