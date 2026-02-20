// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketFabrics    = []byte("fabrics")
	bucketNodes      = []byte("nodes")
	bucketResumption = []byte("resumption")
	bucketKV         = []byte("kv")
)

// BoltStore is a persistent Store backed by a BoltDB database file.
type BoltStore struct {
	db *bolt.DB
}

// DefaultDBPath returns the default database file path, respecting
// XDG_CONFIG_HOME. The default location is ~/.config/matter-cli/matter.db.
func DefaultDBPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("store: determine home dir: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "matter-cli", "matter.db"), nil
}

// NewBoltStore opens or creates a BoltDB-backed store at the given path. The
// parent directory is created automatically if it does not exist.
func NewBoltStore(path string) (*BoltStore, error) {
	return newBoltStore(path, 0)
}

// NewBoltStoreTimeout opens or creates a BoltDB-backed store at the given path
// with a lock-acquisition timeout. If the database file is locked by another
// process and the timeout elapses before the lock is obtained, an error is
// returned immediately. A timeout of 0 means wait indefinitely.
//
// Use this for code paths where blocking is unacceptable (e.g. shell
// completion subprocesses) so they degrade gracefully instead of hanging.
func NewBoltStoreTimeout(path string, timeout time.Duration) (*BoltStore, error) {
	return newBoltStore(path, timeout)
}

func newBoltStore(path string, timeout time.Duration) (*BoltStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: create config dir: %w", err)
	}
	var opts *bolt.Options
	if timeout > 0 {
		opts = &bolt.Options{Timeout: timeout}
	}
	db, err := bolt.Open(path, 0o600, opts)
	if err != nil {
		return nil, fmt.Errorf("store: open database: %w", err)
	}
	// Pre-create top-level buckets.
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketFabrics, bucketNodes, bucketResumption, bucketKV} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("store: init buckets: %w", err)
	}
	return &BoltStore{db: db}, nil
}

func fabricKey(id uint64) []byte {
	return []byte(strconv.FormatUint(id, 10))
}

func nodeKey(nodeID uint64) []byte {
	return []byte(strconv.FormatUint(nodeID, 10))
}

func (s *BoltStore) SaveFabric(fabric *Fabric) error {
	if fabric == nil {
		return fmt.Errorf("store: fabric must not be nil")
	}
	data, err := json.Marshal(fabric)
	if err != nil {
		return fmt.Errorf("store: marshal fabric: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketFabrics).Put(fabricKey(fabric.ID), data)
	})
}

func (s *BoltStore) GetFabric(fabricID uint64) (*Fabric, error) {
	var f Fabric
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketFabrics).Get(fabricKey(fabricID))
		if data == nil {
			return fmt.Errorf("store: fabric %d: %w", fabricID, ErrNotFound)
		}
		return json.Unmarshal(data, &f)
	})
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *BoltStore) ListFabrics() ([]*Fabric, error) {
	var out []*Fabric
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketFabrics).ForEach(func(_, v []byte) error {
			var f Fabric
			if err := json.Unmarshal(v, &f); err != nil {
				return fmt.Errorf("store: unmarshal fabric: %w", err)
			}
			out = append(out, &f)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []*Fabric{}
	}
	return out, nil
}

func (s *BoltStore) DeleteFabric(fabricID uint64) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		key := fabricKey(fabricID)
		b := tx.Bucket(bucketFabrics)
		if b.Get(key) == nil {
			return fmt.Errorf("store: fabric %d: %w", fabricID, ErrNotFound)
		}
		if err := b.Delete(key); err != nil {
			return err
		}
		// Also remove all nodes under this fabric.
		nb := tx.Bucket(bucketNodes)
		sub := nb.Bucket(key)
		if sub != nil {
			return nb.DeleteBucket(key)
		}
		return nil
	})
}

func (s *BoltStore) SaveNode(fabricID uint64, node *Node) error {
	if node == nil {
		return fmt.Errorf("store: node must not be nil")
	}
	node.FabricID = fabricID
	data, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("store: marshal node: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		// Verify fabric exists.
		if tx.Bucket(bucketFabrics).Get(fabricKey(fabricID)) == nil {
			return fmt.Errorf("store: fabric %d: %w", fabricID, ErrNotFound)
		}
		sub, err := tx.Bucket(bucketNodes).CreateBucketIfNotExists(fabricKey(fabricID))
		if err != nil {
			return fmt.Errorf("store: create node sub-bucket: %w", err)
		}
		return sub.Put(nodeKey(node.ID), data)
	})
}

func (s *BoltStore) GetNode(fabricID uint64, nodeID uint64) (*Node, error) {
	var n Node
	err := s.db.View(func(tx *bolt.Tx) error {
		sub := tx.Bucket(bucketNodes).Bucket(fabricKey(fabricID))
		if sub == nil {
			return fmt.Errorf("store: node %d in fabric %d: %w", nodeID, fabricID, ErrNotFound)
		}
		data := sub.Get(nodeKey(nodeID))
		if data == nil {
			return fmt.Errorf("store: node %d in fabric %d: %w", nodeID, fabricID, ErrNotFound)
		}
		return json.Unmarshal(data, &n)
	})
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *BoltStore) ListNodes(fabricID uint64) ([]*Node, error) {
	var out []*Node
	err := s.db.View(func(tx *bolt.Tx) error {
		// Verify fabric exists.
		if tx.Bucket(bucketFabrics).Get(fabricKey(fabricID)) == nil {
			return fmt.Errorf("store: fabric %d: %w", fabricID, ErrNotFound)
		}
		sub := tx.Bucket(bucketNodes).Bucket(fabricKey(fabricID))
		if sub == nil {
			return nil
		}
		return sub.ForEach(func(_, v []byte) error {
			var n Node
			if err := json.Unmarshal(v, &n); err != nil {
				return fmt.Errorf("store: unmarshal node: %w", err)
			}
			out = append(out, &n)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []*Node{}
	}
	return out, nil
}

func (s *BoltStore) DeleteNode(fabricID uint64, nodeID uint64) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		sub := tx.Bucket(bucketNodes).Bucket(fabricKey(fabricID))
		if sub == nil {
			return fmt.Errorf("store: node %d in fabric %d: %w", nodeID, fabricID, ErrNotFound)
		}
		if sub.Get(nodeKey(nodeID)) == nil {
			return fmt.Errorf("store: node %d in fabric %d: %w", nodeID, fabricID, ErrNotFound)
		}
		return sub.Delete(nodeKey(nodeID))
	})
}

func (s *BoltStore) SaveResumptionInfo(info *ResumptionInfo) error {
	if info == nil {
		return fmt.Errorf("store: resumption info must not be nil")
	}
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("store: marshal resumption info: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketResumption).Put(fabricKey(info.PeerNodeID), data)
	})
}

func (s *BoltStore) GetResumptionInfo(peerNodeID uint64) (*ResumptionInfo, error) {
	var info ResumptionInfo
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketResumption).Get(fabricKey(peerNodeID))
		if data == nil {
			return fmt.Errorf("store: resumption info for peer %d: %w", peerNodeID, ErrNotFound)
		}
		return json.Unmarshal(data, &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (s *BoltStore) Set(key string, value []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketKV).Put([]byte(key), value)
	})
}

func (s *BoltStore) Get(key string) ([]byte, error) {
	var out []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketKV).Get([]byte(key))
		if data == nil {
			return fmt.Errorf("store: key %q: %w", key, ErrNotFound)
		}
		out = append([]byte(nil), data...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Close closes the underlying BoltDB database.
func (s *BoltStore) Close() error {
	return s.db.Close()
}
