// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/p0fi/matter-cli/internal/crypto"
	"github.com/p0fi/matter-cli/internal/store"
)

// fabricIdentity holds the controller's fabric keys and certificates.
type fabricIdentity struct {
	rootKey            *ecdsa.PrivateKey
	icacKey            *ecdsa.PrivateKey
	nodeKey            *ecdsa.PrivateKey
	rcac               []byte // DER
	icac               []byte // DER
	noc                []byte // DER
	nocTLV             []byte // Matter TLV encoded
	icacTLV            []byte // Matter TLV encoded
	ipk                []byte // 16-byte raw Identity Protection Key (epoch key)
	operationalIPK     []byte // 16-byte operational IPK derived via HKDF
	rootPubKey         []byte // uncompressed SEC1
	compressedFabricID []byte // 8-byte compressed fabric identifier
}

// initFabric loads an existing fabric from the store, or creates a new one if
// it does not exist.
func (c *Controller) initFabric() error {
	if c.store == nil {
		return nil
	}

	f, err := c.store.GetFabric(c.fabricID)
	if err == nil {
		return c.loadFabric(f)
	}

	// Fabric doesn't exist — create a new one.
	return c.createFabric()
}

// loadFabric reconstructs fabric identity from a stored Fabric record.
func (c *Controller) loadFabric(f *store.Fabric) error {
	// Parse root key from PEM.
	rootKey, err := parsePEMPrivateKey(f.PrivateKeyPEM)
	if err != nil {
		return fmt.Errorf("parsing root private key: %w", err)
	}

	// Parse RCAC from PEM.
	rcac, err := parsePEMCert(f.RootCertPEM)
	if err != nil {
		return fmt.Errorf("parsing RCAC: %w", err)
	}

	// Parse ICAC from PEM.
	icac, err := parsePEMCert(f.ICACertPEM)
	if err != nil {
		return fmt.Errorf("parsing ICAC: %w", err)
	}

	// Load IPK from KV store.
	ipk, err := c.store.Get(fmt.Sprintf("fabric:%d:ipk", c.fabricID))
	if err != nil {
		return fmt.Errorf("loading IPK: %w", err)
	}

	// Load ICAC key from KV store.
	icacKeyPEM, err := c.store.Get(fmt.Sprintf("fabric:%d:icac_key", c.fabricID))
	if err != nil {
		return fmt.Errorf("loading ICAC key: %w", err)
	}
	icacKey, err := parsePEMPrivateKey(string(icacKeyPEM))
	if err != nil {
		return fmt.Errorf("parsing ICAC key: %w", err)
	}

	// Load node key from KV store.
	nodeKeyPEM, err := c.store.Get(fmt.Sprintf("fabric:%d:node_key", c.fabricID))
	if err != nil {
		return fmt.Errorf("loading node key: %w", err)
	}
	nodeKey, err := parsePEMPrivateKey(string(nodeKeyPEM))
	if err != nil {
		return fmt.Errorf("parsing node key: %w", err)
	}

	// Load NOC from KV store.
	noc, err := c.store.Get(fmt.Sprintf("fabric:%d:noc", c.fabricID))
	if err != nil {
		return fmt.Errorf("loading NOC: %w", err)
	}

	// Pre-compute Matter TLV versions for CASE handshake.
	// Preserve original DER signature — the CHIP SDK reconstructs DER TBS from
	// TLV fields for verification, so the original signature is correct.
	nocTLV, err := crypto.X509ToMatterCert(noc)
	if err != nil {
		return fmt.Errorf("converting NOC to Matter TLV: %w", err)
	}
	icacTLV, err := crypto.X509ToMatterCert(icac)
	if err != nil {
		return fmt.Errorf("converting ICAC to Matter TLV: %w", err)
	}

	rootPubKey := crypto.PublicKeyToUncompressed(&rootKey.PublicKey)

	// Derive operational IPK from raw IPK via compressed fabric ID.
	compressedID, err := crypto.CompressedFabricID(rootPubKey, c.fabricID)
	if err != nil {
		return fmt.Errorf("computing compressed fabric ID: %w", err)
	}
	operationalIPK, err := crypto.DeriveGroupOperationalKey(ipk, compressedID)
	if err != nil {
		return fmt.Errorf("deriving operational IPK: %w", err)
	}

	c.fabric = &fabricIdentity{
		rootKey:            rootKey,
		icacKey:            icacKey,
		nodeKey:            nodeKey,
		rcac:               rcac,
		icac:               icac,
		noc:                noc,
		nocTLV:             nocTLV,
		icacTLV:            icacTLV,
		ipk:                ipk,
		operationalIPK:     operationalIPK,
		rootPubKey:         rootPubKey,
		compressedFabricID: compressedID,
	}
	return nil
}

// createFabric generates a new fabric identity: root key, ICAC key, node key,
// RCAC, ICAC, NOC, and IPK. Everything is persisted to the store.
func (c *Controller) createFabric() error {
	opts := crypto.DefaultCertificateOptions()

	// Generate root CA key pair.
	rootKey, err := crypto.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("generating root key: %w", err)
	}

	// Generate RCAC.
	rcac, err := crypto.GenerateRCAC(rootKey, c.fabricID, c.fabricID, opts)
	if err != nil {
		return fmt.Errorf("generating RCAC: %w", err)
	}

	// Generate ICAC key pair.
	icacKey, err := crypto.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("generating ICAC key: %w", err)
	}

	// Generate ICAC.
	icac, err := crypto.GenerateICAC(icacKey, c.fabricID+1, c.fabricID, rcac, rootKey, opts)
	if err != nil {
		return fmt.Errorf("generating ICAC: %w", err)
	}

	// Generate node key pair (controller's own operational identity).
	nodeKey, err := crypto.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("generating node key: %w", err)
	}

	// Generate NOC for the controller node (node ID = fabricID for the controller itself).
	controllerNodeID := c.fabricID
	noc, err := crypto.GenerateNOC(nodeKey, controllerNodeID, c.fabricID, icac, icacKey, opts)
	if err != nil {
		return fmt.Errorf("generating NOC: %w", err)
	}

	// Generate random IPK (16 bytes).
	ipk := make([]byte, 16)
	if _, err := rand.Read(ipk); err != nil {
		return fmt.Errorf("generating IPK: %w", err)
	}

	// Persist to store.
	rootKeyPEM, err := marshalPEMPrivateKey(rootKey)
	if err != nil {
		return fmt.Errorf("marshaling root key: %w", err)
	}

	f := &store.Fabric{
		ID:            c.fabricID,
		Label:         fmt.Sprintf("matter-cli-%d", c.fabricID),
		RootCertPEM:   marshalPEMCert(rcac),
		ICACertPEM:    marshalPEMCert(icac),
		PrivateKeyPEM: rootKeyPEM,
		FabricIndex:   1,
	}

	if err := c.store.SaveFabric(f); err != nil {
		return fmt.Errorf("saving fabric: %w", err)
	}

	// Store additional keys in KV.
	if err := c.store.Set(fmt.Sprintf("fabric:%d:ipk", c.fabricID), ipk); err != nil {
		return fmt.Errorf("saving IPK: %w", err)
	}

	icacKeyPEM, err := marshalPEMPrivateKey(icacKey)
	if err != nil {
		return fmt.Errorf("marshaling ICAC key: %w", err)
	}
	if err := c.store.Set(fmt.Sprintf("fabric:%d:icac_key", c.fabricID), []byte(icacKeyPEM)); err != nil {
		return fmt.Errorf("saving ICAC key: %w", err)
	}

	nodeKeyPEM, err := marshalPEMPrivateKey(nodeKey)
	if err != nil {
		return fmt.Errorf("marshaling node key: %w", err)
	}
	if err := c.store.Set(fmt.Sprintf("fabric:%d:node_key", c.fabricID), []byte(nodeKeyPEM)); err != nil {
		return fmt.Errorf("saving node key: %w", err)
	}

	if err := c.store.Set(fmt.Sprintf("fabric:%d:noc", c.fabricID), noc); err != nil {
		return fmt.Errorf("saving NOC: %w", err)
	}

	// Pre-compute Matter TLV versions for CASE handshake.
	nocTLV, tlvErr := crypto.X509ToMatterCert(noc)
	if tlvErr != nil {
		return fmt.Errorf("converting NOC to Matter TLV: %w", tlvErr)
	}
	icacTLV, tlvErr := crypto.X509ToMatterCert(icac)
	if tlvErr != nil {
		return fmt.Errorf("converting ICAC to Matter TLV: %w", tlvErr)
	}

	rootPubKey := crypto.PublicKeyToUncompressed(&rootKey.PublicKey)

	// Derive operational IPK from raw IPK via compressed fabric ID.
	compressedID, cfErr := crypto.CompressedFabricID(rootPubKey, c.fabricID)
	if cfErr != nil {
		return fmt.Errorf("computing compressed fabric ID: %w", cfErr)
	}
	operationalIPK, opErr := crypto.DeriveGroupOperationalKey(ipk, compressedID)
	if opErr != nil {
		return fmt.Errorf("deriving operational IPK: %w", opErr)
	}

	c.fabric = &fabricIdentity{
		rootKey:            rootKey,
		icacKey:            icacKey,
		nodeKey:            nodeKey,
		rcac:               rcac,
		icac:               icac,
		noc:                noc,
		nocTLV:             nocTLV,
		icacTLV:            icacTLV,
		ipk:                ipk,
		operationalIPK:     operationalIPK,
		rootPubKey:         rootPubKey,
		compressedFabricID: compressedID,
	}
	return nil
}

// marshalPEMPrivateKey encodes an ECDSA private key as PEM.
func marshalPEMPrivateKey(key *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	return string(pem.EncodeToMemory(block)), nil
}

// parsePEMPrivateKey decodes a PEM-encoded ECDSA private key.
func parsePEMPrivateKey(pemStr string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not ECDSA")
	}
	return ecKey, nil
}

// marshalPEMCert encodes a DER certificate as PEM.
func marshalPEMCert(der []byte) string {
	block := &pem.Block{Type: "CERTIFICATE", Bytes: der}
	return string(pem.EncodeToMemory(block))
}

// parsePEMCert decodes a PEM-encoded certificate and returns the DER bytes.
func parsePEMCert(pemStr string) ([]byte, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	return block.Bytes, nil
}
