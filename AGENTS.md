# AGENTS.md — Matter CLI: Agent Team Orchestration Plan

## Project Overview

**Goal:** Build `matter-cli`, a pure Go Matter controller CLI with modern ergonomics, inspired by GitHub CLI (`gh`). No C++ dependencies, no ZAP tooling. Single static binary.

**Repository structure:**

```
matter-cli/
├── cmd/
│   └── matter/
│       └── main.go
├── internal/
│   ├── tlv/              # Matter TLV codec
│   ├── crypto/           # SPAKE2+, CASE/SIGMA, AES-CCM, certs
│   ├── transport/        # UDP, MRP, connection management
│   ├── protocol/         # Message framing, exchange manager, sessions
│   ├── secure/           # PASE & CASE session establishment
│   ├── interaction/      # Interaction Model (read/write/invoke/subscribe)
│   ├── commissioning/    # Commissioning orchestrator
│   ├── discovery/        # mDNS discovery
│   ├── store/            # Persistent storage (fabric, nodes, sessions)
│   ├── clusters/         # Cluster definitions & registry
│   │   ├── registry.go   # Central cluster/attribute/command registry
│   │   ├── onoff/
│   │   ├── levelcontrol/
│   │   ├── colorcontrol/
│   │   ├── doorlock/
│   │   ├── thermostat/
│   │   ├── windowcovering/
│   │   ├── basicinformation/
│   │   ├── descriptor/
│   │   ├── generalcommissioning/
│   │   ├── networkcommissioning/
│   │   ├── operationalcredentials/
│   │   ├── accesscontrol/
│   │   └── identify/
│   └── codegen/          # Cluster code generator (reads Matter spec XML/IDL)
├── pkg/
│   └── matter/           # Public API for embedding matter-cli as a library
├── cli/
│   ├── root.go           # Root cobra command
│   ├── commission.go
│   ├── cluster.go
│   ├── discover.go
│   ├── device.go         # `ls`, `inspect` commands
│   ├── interactive.go    # REPL mode
│   ├── config.go
│   ├── payload.go
│   ├── output/           # Output formatting (table, json, yaml)
│   │   ├── formatter.go
│   │   ├── table.go
│   │   ├── json.go
│   │   └── color.go
│   └── completion/       # Dynamic completions
│       └── completer.go
├── testdata/
│   ├── tlv/              # TLV test vectors
│   ├── crypto/           # SPAKE2+, CASE test vectors
│   ├── pcap/             # Captured Matter packet dumps
│   └── fixtures/         # Mock device state
├── docs/
│   ├── architecture.md
│   └── spec-references.md
├── go.mod
├── go.sum
├── Makefile
└── agents.md             # This file
```

## Matter Specification Source for Implementation
You can find the official Matter specification source files in `../connectedhomeip-spec/`. Those are the canonical references for all protocol details, TLV encoding rules, cryptographic algorithms, and cluster definitions. The C++ Matter SDK (`connectedhomeip/`) is the reference implementation, but the spec source files are the ultimate authority.

## Design Principles (All Agents Must Follow)

### 1. Idiomatic Go
- Use `io.Reader`/`io.Writer` interfaces for all encoding/decoding
- Errors are values — use `fmt.Errorf("doing X: %w", err)` wrapping consistently
- No `panic` outside of init-time programmer errors
- Prefer table-driven tests with `t.Run` subtests
- Use `context.Context` for all operations that touch network or may block
- Naming: `MarshalTLV`/`UnmarshalTLV`, not `Encode`/`Decode` (follow `encoding/json` patterns)
- Use `internal/` for all packages not intended for external consumption
- Interfaces should be small (1-3 methods), defined by the consumer
- Zero values should be useful

### 2. Testability
- Every package must have `_test.go` files with >80 % coverage
- No global state. All dependencies injected via constructors or functional options
- Network-touching code must accept interfaces (e.g., `transport.Conn`) so tests can use mocks
- Use `testing/fstest` or interfaces for filesystem access
- Crypto code must validate against known test vectors from the Matter spec and SDK test suites
- Include integration test tags: `//go:build integration`

### 3. CLI Ergonomics (GitHub CLI Style)
- Use `github.com/spf13/cobra` for command structure
- Use `github.com/spf13/viper` for configuration
- Use `github.com/charmbracelet/huh` for interactive prompts
- Use `github.com/charmbracelet/lipgloss` for styled output
- Use `github.com/charmbracelet/bubbletea` for the interactive REPL
- Use `github.com/charmbracelet/bubbles` for REPL components (text input, tables, spinners)
- All commands support `--format json|table|yaml` (default: `table` for TTY, `json` for pipes)
- Use `--node` / `-n` for node ID, `--endpoint` / `-e` for endpoint (short flags everywhere)
- Colors respect `NO_COLOR` env var
- Errors print to stderr with suggestions for next steps

### 4. No ZAP
- Cluster definitions are hand-written Go or generated from Matter spec `.matter` IDL files
- The code generator in `internal/codegen/` reads `.matter` IDL files from the `connectedhomeip` repo's `src/app/clusters/` or `src/controller/data_model/` directories
- Each cluster is a self-contained Go package with struct definitions, TLV tags, and CLI registration

### 5. Human-Readable Everything
- All cluster IDs, attribute IDs, command IDs have string name mappings in the registry
- CLI accepts both names and hex IDs: `--cluster on-off` OR `--cluster 0x0006`
- Interactive mode always shows human names with IDs in parentheses
- Device inspection shows the full tree with names


### 6. Reference Implementations
- C++ reference: `https://github.com/project-chip/connectedhomeip` (the official Matter SDK)
- Go reference: `https://github.com/tom-code/gomat` (a pure Go Matter implementation by Tom Code, stale but good for reference)
- JS/TS reference: `https://github.com/matter-js/matter.js` (a JavaScript/TypeScript Matter implementation)
---

## Agent Definitions

### Agent 1: TLV (Foundation)

**Scope:** `internal/tlv/`

**Responsibility:** Implement the Matter TLV (Tag-Length-Value) encoding specified in Matter spec Appendix A.

**Files to produce:**
- `internal/tlv/types.go` — Element types, tag types, constants
- `internal/tlv/encoder.go` — `Writer` that encodes Go values to TLV bytes
- `internal/tlv/decoder.go` — `Reader` that decodes TLV bytes to Go values
- `internal/tlv/marshal.go` — Struct tag-based `Marshal(v any) ([]byte, error)` / `Unmarshal(data []byte, v any) error`
- `internal/tlv/tags.go` — Tag encoding/decoding (anonymous, context-specific, common profile, vendor)
- `internal/tlv/types_test.go`
- `internal/tlv/encoder_test.go`
- `internal/tlv/decoder_test.go`
- `internal/tlv/marshal_test.go`
- `internal/tlv/roundtrip_test.go`

**Spec references:**
- Matter Specification, Appendix A: "Tag-Length-Value (TLV) Encoding"
- C++ reference: `connectedhomeip/src/lib/core/TLV*.h` and `TLV*.cpp`
- Go reference: `github.com/tom-code/gomat/mattertlv/`


**Key design decisions:**
- Use Go struct tags: `tlv:"contextTag,type"` e.g., `tlv:"1,uint"`, `tlv:"2,utf8"`, `tlv:"3,struct"`
- Support all Matter TLV types: signed int (1/2/4/8), unsigned int (1/2/4/8), bool, float32, float64, UTF-8 string, octet string, null, structure, array, list
- `Writer` uses a `bytes.Buffer` internally, supports nested containers via

 `StartStructure(tag)`/`EndContainer()`
- `Reader` is streaming — reads from `io.Reader`, one element at a time
- `Marshal`/`Unmarshal` use reflection with struct tags (like `encoding/json`)
- All integers are little-endian per Matter spec
- Null handling: pointer fields in structs → `*uint32` is nullable

**Test vectors (from spec & SDK):**
```
// Simple unsigned integer
{Tag: ContextTag(1), Value: uint8(42)} → [0x24, 0x01, 0x2A]

// Structure with nested fields
Structure {
  ContextTag(0): true,
  ContextTag(1): uint32(1),
} → [0x15, 0x29, 0x00, 0x26, 0x01, 0x01, 0x00, 0x00, 0x00, 0x18]

// UTF-8 string
{Tag: ContextTag(2), Value: "Hello"} → [0x2C, 0x02, 0x05, 0x48, 0x65, 0x6C, 0x6C, 0x6F]
```

Extract additional test vectors from `connectedhomeip/src/lib/core/tests/TestTLVReader.cpp` and `TestTLVWriter.cpp`.

**Completion criteria:**
- [x] All element types encode/decode correctly
- [x] Nested structures, arrays, lists work
- [x] Marshal/Unmarshal with struct tags works for all types
- [x] Round-trip: Marshal → Unmarshal → equal
- [x] Fuzz test: `go test -fuzz` for decoder robustness
- [x] Zero allocations in hot paths (benchmark it)

**Dependencies:** None (leaf package)

**Blocked by:** Nothing — start immediately

---

### Agent 2: Crypto

**Scope:** `internal/crypto/`

**Responsibility:** All cryptographic operations required by Matter: SPAKE2+, CASE key derivation, AES-CCM, certificate handling.

**Files to produce:**
- `internal/crypto/spake2p.go` — SPAKE2+ prover & verifier
- `internal/crypto/spake2p_test.go`
- `internal/crypto/a

esccm.go` — AES-128-CCM encryption/decryption (Go stdlib lacks CCM)
- `internal/crypto/aesccm_test.go`
- `internal/crypto/hkdf.go` — HKDF-SHA256 wrapper with Matter-specific info strings
- `internal/crypto/hkdf_test.go`
- `internal/crypto/keys.go` — P-256 key generation, ECDH, ECDSA
- `internal/crypto/keys_test.go`
- `internal/crypto/certs.go` — Matter Operational Certificate (NOC/ICAC/RCAC) generation & parsing
- `internal/crypto/certs_test.go`
- `internal/crypto/pbkdf2.go` — PBKDF2 for passcode → SPAKE2+ verifier derivation
- `internal/crypto/pbkdf2_test.go`

**Spec references:**
- Matter Specification, Chapter 3: "Cryptographic Primitives"
- Matter Specification, Chapter 4: "Security" (SPAKE2+, CASE)
- IETF draft-bar-cfrg-spake2plus-01
- C++ reference: `connectedhomeip/src/crypto/CHIPCryptoPAL*.cpp`
- Go reference: `github.com/tom-code/gomat/crypto.go`, `gomat/spake2p.go`

**Key design decisions:**
- AES-CCM:

 Implement from AES-CTR + CBC-MAC per RFC 3610. Do NOT use a random library — implement against the RFC and test with NIST vectors
- SPAKE2+: Use the Matter-specified M and N points for P-256 (they're fixed constants in the spec)
- PBKDF2: Use `golang.org/x/crypto/pbkdf2` with SHA-256, iterations from commissioning parameters
- X.509 certificates: Use Go's `crypto/x509` for DER encoding/decoding. Matter uses standard X.509v3 with specific extensions (vendor ID, product ID in subject)
- Key storage: Keys are `crypto.PrivateKey` interfaces. Persist via PEM or raw bytes — handled by `store` package, not here
- All functions take/return `[]byte` or standard `crypto` interfaces — no custom key types

**SPAKE2+ algorithm summary for implementation:**
```
Inputs: passcode (uint32), salt ([]byte), iterations (uint32)
1. w0, w1 = PBKDF2(passcode, salt, iterations) → split into two P-256 scalars
2. Prover: x ← random scalar, X = x*G + w0*M → send pA = X
3. Verifier: y ← random scalar, Y = y*G + w0*N → send pB = Y  
4. Prover: Z = h*x*(Y - w0*N), V = h*w1*(Y - w0*N)
5. Verifier: Z = h*y*(X - w0*M), V = h*y*L  (where L = w1*G, precomputed)
6. TT = Hash(context || idProver || idVerifier || M || N || X || Y || Z || V || w0)
7. Ka || Ke = KDF(TT)
8. KcA || KcB = KDF(Ka)
9. cA = HMAC(KcA, pB), cB = HMAC(KcB, pA) — confirmation values
```

**Test vectors:**
- Extract from `connectedhomeip/src/crypto/tests/CHIPCryptoPALTest.cpp`
- NIST AES-CCM test vectors from NIST SP 800-38C
- SPAKE2+ test vectors from the IETF draft

**Completion criteria:**
- [x] SPAKE2+ prover & verifier produce matching keys
- [x] AES-CCM encrypts/decrypts correctly against NIST vectors
- [x] HKDF produces correct output against RFC 5869 test vectors
- [x] X.509 certificate generation produces certs that OpenSSL can parse
- [x] All test vectors from Matter SDK pass
- [x] Constant-time comparison for all MAC checks

**Dependencies:** None (uses Go stdlib `crypto` only)

**Blocked by:** Nothing — start immediately, parallel with Agent

 1

---

### Agent 3: Protocol (Message Layer)

**Scope:** `internal/transport/`, `internal/protocol/`

**Responsibility:** Matter message framing, UDP transport, MRP (Message Reliability Protocol), exchange management, session table.

**Files to produce:**
- `internal/transport/udp.go` — UDP socket abstraction (send/receive Matter messages)
- `internal/transport/udp_test.go`
- `internal/transport/conn.go` — `Conn` interface for testability
- `internal/transport/mrp.go` — Message Reliability Protocol (retransmission, ACK tracking)
- `internal/transport/mrp_test.go`
- `internal/protocol/message.go` — Matter message header encode/decode
- `internal/protocol/message_test.go`
- `internal/protocol/exchange.go` — Exchange manager (create, track, route exchanges)
- `internal/protocol/exchange_test.go`
- `internal/protocol/session.go` — Session table (unsecured, PASE, CASE sessions)
- `internal/protocol/session_test.go`
- `internal/protocol/codec.go` — Full message encode (header + payload + encrypt) / decode (decrypt + parse)
- `internal/protocol/codec_test.go`

**Spec references:**
- Matter Specification, Chapter 4.4: "Message Format"
- Matter Specification, Chapter 4.11: "Message Reliability Protocol"
- Matter Specification, Chapter 4.10: "Session Establishment"
- C++ reference: `connectedhomeip/src/transport/` and `connectedhomeip/src/messaging/`

**Message header format:**
```
Byte 0:     Message Flags
              Bit 0-1: DSIZ (destination node ID size: 0=none, 1=64bit, 2=16bit group)
              Bit 2:   S flag (source node ID present)
              Bit 4:   V flag (version, must be 0)
Byte 1-2:   Session ID (uint16 LE)
Byte 3:     Security Flags
              Bit 0-1: Session Type (0=unicast, 1=group)
              Bit 3:   MX flag (Message Extensions present)
              Bit 5:   C flag (message is a counter-sync)
              Bit 7:   P flag (privacy encoding)
Byte 4-7:   Message Counter (uint32 LE)
[Optional]  Source Node ID (uint64 LE, if S flag)
[Optional]  Destination Node ID (uint64 LE or uint16 LE based on DSIZ)

Protocol Header (within decrypted payload):
Byte 0:     Exchange Flags
              Bit 0: I flag (Initiator)
              Bit 1: A flag (ACK)
              Bit 2: R flag (Reliability, needs ACK)
              Bit 3: SX flag (Secured Extensions)
              Bit 4: V flag (Vendor Protocol)
Byte 1:     Protocol Opcode
Byte 2-3:   Exchange ID (uint16 LE)
Byte 4-5:   Protocol ID (uint16 LE)
[Optional]  Vendor ID (uint16 LE, if V flag)
[Optional]  ACK Message Counter (uint32 LE, if A flag)
```

**Key design decisions:**
- `transport.Conn` interface:
  ```go
  type Conn interface {
      Send(ctx context.Context, msg []byte, addr net.Addr) error
      Receive(ctx context.Context) ([]byte, net.Addr, error)
      Close() error
  }
  ```
- MRP: Track per-exchange. Configurable idle/active retransmit timeouts. Use `time.Timer` not goroutine-per-message
- Exchange Manager: 
  ```go
  type ExchangeManager struct { ... }
  func (em *ExchangeManager) NewExchange(ctx context.Context, session *Session) (*Exchange, error)
  func (em *ExchangeManager) HandleMessage(msg *Message) error
  ```
- Sessions stored in a concurrent-safe session table (`sync.RWMutex`)
- Message counter validation: track per-peer, reject replays

**Completion criteria:**
- [x] Message header round-trips (encode → decode → equal)
- [x] MRP retransmits on timeout, stops on ACK
- [x] Exchange manager routes responses to correct exchange
- [x] Session table concurrent access safe
- [x] Encrypted message codec works with mock session keys

**Dependencies:** Agent 1 (TLV — for payload encoding), Agent 2 (Crypto — for AES-CCM message encryption)

**Blocked by:** Can start message framing and MRP immediately (no encryption needed for unencrypted messages). Encryption codec blocked on Agent 2 completion.

---

### Agent 4: Secure Sessions (PASE & CASE)

**Scope:** `internal/secure/`

**Responsibility:** Implement PASE (SPAKE2+ based) and CASE (SIGMA based) session establishment protocols.

**Files to produce:**
- `internal/secure/pase.go` — PASE session establishment (commissioner side)
- `internal/secure/pase_test.go`
- `internal/secure/case.go` — CASE (SIGMA) session establishment
- `internal/secure/case_test.go`
- `internal/secure/session_keys.go` — Key derivation from established sessions (I2R, R2I keys)
- `internal/secure/session_keys_test.go`

**Spec references:**
- Matter Specification, Chapter 4.13: "PASE"  
- Matter Specification, Chapter 4.14: "CASE"
- C++ reference: `connectedhomeip/src/protocols/secure_channel/`

**PASE flow (commissioner is initiator):**
```
Commissioner                         Device
    |                                   |
    |--- PBKDFParamRequest ------------>|  (exchange start)
    |<-- PBKDFParamResponse ------------|  (salt, iterations, sessionId)
    |--- PASE_Pake1 (pA) -------------->|  (SPAKE2+ public value X)
    |<-- PASE_Pake2 (pB, cB) -----------|  (SPAKE2+ public value Y + confirmation)
    |--- PASE_Pake3 (cA) -------------->|  (our confirmation)
    |<-- StatusReport -----------.------|  (success/fail)
    |                                   |
    [Session keys derived: I2RKey, R2IKey, AttestationChallenge]
```

**CASE flow (SIGMA):**
```
Commissioner                         Device
    |                                   |
    |--- Sigma1 ----------------------->|  (random, sessionId, destId, resumptionId?)
    |<-- Sigma2 ------------------------|  (random, NOC, ICAC, signature, encrypted)
    |--- Sigma3 ----------------------->|  (NOC, ICAC, signature, encrypted)
    |<-- StatusReport ------------------|  (success/fail)
    |                                   |
    [Session keys derived from ECDH shared secret]
```

**Key design decisions:**
- Both PASE and CASE implement a common interface:


  ```go
  type SessionEstablisher interface {
      Establish(ctx context.Context, exchange *protocol.Exchange) (*protocol.SecureSession, error)
  }
  ```
- PASE constructor takes: `passcode uint32, salt []byte, iterations uint32`
- CASE constructor takes: `fabricPrivKey crypto.PrivateKey, noc, icac, rcac []byte`
- Both produce a `*SecureSession` containing I2R/R2I keys and attestation challenge
- Use `internal/crypto` for all crypto ops — no direct `crypto/*` imports

**Completion criteria:**
- [x] PASE: two in-process instances (prover + verifier) derive matching session keys
- [x] CASE: two in-process instances derive matching session keys
- [x] Integration: PASE over mock transport exchanges correct messages
- [x] Test vectors from Matter SDK test suite pass

**Dependencies:** Agent 1 (TLV), Agent 2 (Crypto), Agent 3 (Protocol — Exchange, Session types)

**Blocked by:** Agent 2 (Crypto — SPAKE2+, AES-CCM), Agent 3 (Protocol — Exchange abstraction)

---

### Agent 5: Interaction Model

**Scope:** `internal/interaction/`

**Responsibility:** Implement the Matter Interaction Model — Read, Write, Invoke, Subscribe operations.

**Files to produce:**
- `internal/interaction/client.go` — IM client that sends requests and processes responses
- `internal/interaction/read.go` — ReadRequest / ReportDataMessage handling
- `internal/interaction/read_test.go`
- `internal/interaction/write.go` — WriteRequest / WriteResponse handling
- `internal/interaction/write_test.go`
- `internal/interaction/invoke.go` — InvokeRequest / InvokeResponse handling
- `internal/interaction/invoke_test.go`
- `internal/interaction/subscribe.go` — SubscribeRequest / SubscribeResponse + ongoing reports
- `internal/interaction/subscribe_test.go`
- `internal/interaction/status.go` — StatusCode enum and StatusResponse parsing
- `internal/interaction/status_test.go`
- `internal/interaction/path.go` — Attribute/Event/Command path types
- `internal/interaction/path_test.go`

**Spec references:**
- Matter Specification, Chapter 8: "Interaction Model"
- Protocol ID: 0x0001 (Interaction Model)
- C++ reference: `connectedhomeip/src/app/ReadClient.cpp`, `WriteClient.cpp`, `CommandSender.cpp`

**IM message opcodes:**
```
StatusResponse      = 0x01
ReadRequest         = 0x02
SubscribeRequest    = 0x03
SubscribeResponse   = 0x04
ReportData          = 0x05
WriteRequest        = 0x06
WriteResponse       = 0x07
InvokeRequest       = 0x08
InvokeResponse      = 0x09
TimedRequest        = 0x0A
```

**Key types:**
```go
type AttributePath struct {
    EndpointID  *uint16  `tlv:"2,uint"`
    ClusterID   *uint32  `tlv:"3,uint"`
    AttributeID *uint32  `tlv:"4,uint"`
}

type CommandPath struct {
    EndpointID uint16  `tlv:"0,uint"`
    ClusterID  uint32  `tlv:"1,uint"`
    CommandID  uint32  `tlv:"2,uint"`
}

type ReadRequest struct {
    AttributeRequests []AttributePath `tlv:"0,array"`
    EventRequests     []EventPath     `tlv:"1,array"`
    FabricFiltered    bool            `tlv:"3,bool"`
}

// Client API
type Client struct { ... }

func (c *Client) Read(ctx context.Context, session *protocol.SecureSession, 
    paths ...AttributePath) ([]AttributeReport, error)

func (c *Client) Write(ctx context.Context, session *protocol.SecureSession,
    writes ...AttributeWrite) ([]AttributeStatus, error)

func (c *Client) Invoke(ctx context.Context, session *protocol.SecureSession,
    path CommandPath, request any) (any, error)

func (c *Client) Subscribe(ctx context.Context, session *protocol.SecureSession,
    paths []AttributePath, minInterval, maxInterval uint16) (*

Subscription, error)

type Subscription struct {
    Reports <-chan AttributeReport
    Errors  <-chan error
    Cancel  func()
}
```

**Key design decisions:**
- `Read` returns fully decoded `[]AttributeReport` with raw TLV data — cluster-specific decoding happens in the cluster packages
- `Subscribe` returns channels — the caller consumes `Reports` in a goroutine
- `Invoke` takes/returns `any` — the caller passes cluster-specific request/response structs that implement TLV marshal
- Timed interactions: supported via `TimedRequest` prefix when `timedInteractionTimeout` is set
- Chunked reads: handle `MoreChunkedMessages` flag, reassemble automatically

**Completion criteria:**
- [x] ReadRequest TLV encoding matches spec examples
- [x] ReportData TLV decoding handles all attribute types
- [x] InvokeRequest/Response round-trip works
- [x] Subscription receives multiple ReportData messages
- [x] Chunked data reassembly works
- [x] All IM StatusCodes mapped to Go errors

**Dependencies:** Agent 1 (TLV), Agent 3 (Protocol — Exchange, SecureSession)

**Blocked by:** Agent 1 (TLV — must be complete), Agent 3 (Protocol — Exchange abstraction)

---

### Agent 6: Cluster Registry & Code Generation

**Scope:** `internal/clusters/`, `internal/codegen/`

**Responsibility:** Define all cluster metadata (names, attribute IDs, command IDs, types) in a central registry. Build a code generator that reads Matter `.matter` IDL files and produces Go cluster packages.

**Files to produce:**
- `internal/clusters/registry.go` — Central registry mapping IDs ↔ names
- `internal/clusters/registry_test.go`
- `internal/clusters/types.go` — Shared types (ClusterID, AttributeID, etc.)
- `internal/codegen/parser.go` — Parser for `.matter` IDL files
- `internal/codegen/parser_test.go`
- `internal/codegen/generator.go` — Go code generator
- `internal/codegen/generator_test.go`
- `internal/codegen/templates/` — Go templates for generated code
- `internal/clusters/<name>/cluster.go` — One per cluster (generated)
- `internal/clusters/<name>/attributes.go` — Attribute structs with TLV tags
- `internal/clusters/<name>/commands.go` — Command request/response structs

**IDL source:** `connectedhomeip/src/controller/data_model/controller-clusters.matter`

This is a single `.matter` file that contains ALL cluster definitions for the controller side. 

**Example `.matter` IDL syntax:**
```
cluster OnOff = 6 {
  revision 6;

  enum DelayedAllOffEffectVariantEnum : enum8 {
    kDelayedOffFastFade = 0;
    kNoFade = 1;
    kDelayedOffSlowFade = 2;
  }

  bitmap Feature : bitmap32 {
    kLighting = 0x1;
    kDeadFrontBehavior = 0x2;
    kOffOnly = 0x4;
  }

  readonly attribute boolean onOff = 0;
  readonly attribute optional boolean globalSceneControl = 16384;
  attribute optional int16u onTime = 16385;
  attribute optional int16u offWaitTime = 16386;
  attribute access(write: manage) optional nullable StartUpOnOffEnum startUpOnOff = 16387;

  command Off(): DefaultSuccess = 0;
  command On(): DefaultSuccess = 1;
  command Toggle(): DefaultSuccess = 2;
  command OffWithEffect(OffWithEffectRequest): DefaultSuccess = 64;
}
```

**Registry design:**
```go
package clusters

type ClusterInfo struct {
    ID         uint32
    Name       string      // "on-off" (kebab-case, used in CLI)
    DisplayName string     // "On/Off" (human-friendly)
    Attributes []AttributeInfo
    Commands   []CommandInfo
}

type AttributeInfo struct {
    ID          uint32
    Name        string     // "on-off" (kebab-case)
    DisplayName string     // "OnOff"
    Type        string     // "bool", "uint16", etc.
    Readable    bool
    Writable    bool
    Optional    bool
    Nullable    bool
}

type CommandInfo struct {
    

ID          uint32
    Name        string     // "toggle" (kebab-case)
    DisplayName string     // "Toggle"
    RequestType  reflect.Type  // nil if no payload
    ResponseType reflect.Type  // nil if DefaultSuccess
}

// Global registry — populated at init() time by each cluster package
var Registry = NewRegistry()

type Registry struct { ... }
func (r *Registry) ClusterByName(name string) (*ClusterInfo, bool)
func (r *Registry) ClusterByID(id uint32) (*ClusterInfo, bool)
func (r *Registry) AttributeByName(clusterID uint32, name string) (*AttributeInfo, bool)
func (r *Registry) CommandByName(clusterID uint32, name string) (*CommandInfo, bool)
func (r *Registry) AllClusters() []ClusterInfo
func (r *Registry) SearchClusters(query string) []ClusterInfo  // fuzzy search for autocomplete
func (r *Registry) SearchAttributes(clusterID uint32, query string) []AttributeInfo
```

**Generated code example (`internal/clusters/onoff/cluster.go`):**
```go
package onoff

import "github.com/<org>/matter-cli/internal/clusters"

const ID clusters.ClusterID = 0x0006
const Name = "on-off"
const DisplayName = "On/Off"

//

 Attributes
const (
    AttrOnOff              clusters.AttributeID = 0x0000
    AttrGlobalSceneControl clusters.AttributeID = 0x4000
    AttrOnTime             clusters.AttributeID = 0x4001
    AttrOffWaitTime        clusters.AttributeID = 0x4002
    AttrStartUpOnOff       clusters.AttributeID = 0x4003
)

// Commands
type OffCommand struct{}
type OnCommand struct{}
type ToggleCommand struct{}
type OffWithEffectRequest struct {
    EffectIdentifier DelayedAllOffEffectVariantEnum `tlv:"0,uint"`
    EffectVariant    uint8                           `tlv:"1,uint"`
}

func init() {
    clusters.Registry.Register(clusters.ClusterInfo{
        ID: uint32(ID),
        Name: Name,
        DisplayName: DisplayName,
        Attributes: []clusters.AttributeInfo{
            {ID: 0x0000, Name: "on-off", DisplayName: "OnOff", Type: "bool", Readable: true},
            // ...
        },
        Commands: []clusters.CommandInfo{
            {ID: 0, Name: "off", DisplayName: "Off"},
            {ID: 1, Name: "on", DisplayName: "On"},
            {ID: 2, Name: "toggle", DisplayName: "Toggle"},
        },
    })
}
```

**Priority clusters to generate first (needed for commissioning):**
1. `general-commissioning` (0x0030)
2. `operational-credentials` (0x003E)  
3. `network-commissioning` (0x0031)
4. `access-control` (0x001F)
5. `basic-information` (0x0028)
6. `descriptor` (0x001D)
7. `on-off` (0x0006) — for testing
8. `identify` (0x0003) — for testing
9. `level-control` (0x0008)
10. `color-control` (0x0300)
11. `door-lock` (0x0101)
12. `thermostat` (0x0201)
13. `window-covering` (0x0102)

**Completion criteria:**
- [x] Parser handles full `.matter` IDL syntax (clusters, structs, enums, bitmaps, commands, attributes, events)
- [x] Generator produces compilable, correctly-tagged Go code
- [x] Generated code registers with the central Registry
- [x] Registry supports lookup by ID and by name (case-insensitive)
- [x] Fuzzy search works for autocomplete use case
- [x] `go generate` command regenerates all cluster code

**Dependencies:** Agent 1 (TLV — struct tags must be finalized)

**Blocked by:** Agent 1 (TLV tag format)

---

### Agent 7: Commissioning & Discovery

**Scope:** `internal/commissioning/`, `internal/discovery/`

**Responsibility:** Implement mDNS-based device discovery and the full Matter commissioning flow.

**Files to produce:**
- `internal/discovery/mdns.go` — mDNS browser for `_matterc._udp` (commissionable) and `_matter._tcp` (operational)
- `internal/discovery/mdns_test.go`
- `internal/discovery/device.go` — Discovered device type with parsed TXT records
- `internal/discovery/device_test.go`
- `internal/commissioning/flow.go` — Commissioning orchestrator (the full sequence)
- `internal/commissioning/flow_test.go`
- `internal/commissioning/attestation.go` — Device Attestation Procedure (DAC chain validation)
- `internal/commissioning/attestation_test.go`
- `internal/commissioning/network.go` — Network provisioning (WiFi/Thread credentials)
- `internal/commissioning/network_test.go`
- `internal/commissioning/payload.go` — QR code and Manual Pairing Code parser/generator
- `internal/commissioning/payload_test.go`

**External dependency:** `github.com/hashicorp/mdns` or `github.com/grandcat/zeroconf`

**Spec references:**
- Matter Specification, Chapter 5: "Commissioning"
- Matter Specification, Chapter 5.1.3: "Onboarding Payload" (QR codes, manual codes)
- Matter Specification, Chapter 6: "Device Discovery"



**Commissioning flow (orchestrator):**
```go
type Commissioner struct {
    fabricKey    crypto.PrivateKey
    rcac, icac   []byte
    store        store.Store
    discovery    *discovery.Browser
}

type CommissioningParams struct {
    SetupCode     string    // "MT:..." QR code or "1234-567-8901" manual code
    NodeID        uint64
    WiFiSSID      string    // optional, for WiFi commissioning
    WiFiPassword  string
    ThreadDataset []byte    // optional, for Thread commissioning
}

func (c *Commissioner) Commission(ctx context.Context, params CommissioningParams) error {
    // 1. Parse setup code → discriminator, passcode, discovery hint
    // 2. Discover device via mDNS using discriminator
    // 3. Establish PASE session using passcode
    // 4. Read BasicInformation cluster (VID, PID, device name)
    // 5. Device

 Attestation: read DAC, PAI → validate chain → verify attestation
    // 6. CSR: send CSRRequest to device → get CSR
    // 7. Generate NOC signed by our fabric CA → send AddNOC
    // 8. Configure regulatory info, set fabric label
    // 9. If WiFi/Thread: send network credentials via NetworkCommissioning cluster
    // 10. CommissioningComplete command
    // 11. Close PASE session
    // 12. Discover device on operational network (mDNS _matter._tcp)
    // 13. Establish CASE session (validates our NOC)
    // 14. Read Descriptor cluster to get device structure
    // 15. Store device info in local database
    // 16. Return success
}
```

**QR Code / Manual Pairing Code parsing:**
```
QR Code format:  "MT:Y3.13O

TB00KA0648G00"
  Contains: version, VID, PID, commissioning flow, discriminator (12-bit), passcode (27-bit)
  Encoding: base38 over a packed bit field

Manual Code format: "34970112332"
  Contains: discriminator (4-bit short), passcode (27-bit) 
  Encoding: decimal with check digit
```

**mDNS discovery TXT record fields:**
```
_matterc._udp.local:
  D=<discriminator>       (long: 12-bit)
  CM=<commissioning-mode> (0=not, 1=basic, 2=enhanced)
  VP=<VID>+<PID>
  AP=<additional-pairing> 
  SII=<session-idle-interval>
  SAI=<session-active-interval>
```

**Completion criteria:**
- [x] QR code and manual pairing code parse/generate correctly (test with known codes)
- [x] mDNS discovers commissionable devices on local network
- [x] Full commissioning flow works against a real Matter device (integration test)
- [x] Device Attestation validates a valid DAC chain and rejects invalid ones
- [x] Commissioned devices are persisted in the store

**Dependencies:** Agent 1 (TLV), Agent 2 (Crypto), Agent 3 (Protocol), Agent 4 (Secure Sessions — PASE/CASE), Agent 5 (Interaction Model), Agent 6 (Clusters — GeneralCommissioning, OperationalCredentials, NetworkCommissioning)

**Blocked by:** Agents 4, 5, 6 (needs all protocol layers working)

---

### Agent 8: Storage

**Scope:** `internal/store/`

**Responsibility:** Persistent storage for fabrics, commissioned devices, sessions, and configuration.

**Files to produce:**
- `internal/store/store.go` — `Store` interface definition
- `internal/store/bolt.go` — BoltDB-backed implementation
- `internal/store/bolt_test.go`
- `

internal/store/memory.go` — In-memory implementation for tests
- `internal/store/memory_test.go`
- `internal/store/types.go` — Stored entity types (Fabric, Node, etc.)

**Key types:**
```go
type Store interface {
    // Fabric management
    SaveFabric(fabric *Fabric) error
    GetFabric(fabricID uint64) (*Fabric, error)
    ListFabrics() ([]*Fabric, error)
    DeleteFabric(fabricID uint64) error

    // Node (commissioned device) management
    SaveNode(fabricID uint64, node *Node) error
    GetNode(fabricID uint64, nodeID uint64) (*Node, error)
    ListNodes(fabricID uint64) ([]*Node, error)
    DeleteNode(fabricID uint64, nodeID uint64) error

    // Session resumption data
    SaveResumptionInfo(info *ResumptionInfo) error
    GetResumptionInfo(peerNodeID uint64) (*ResumptionInfo, error)

    // Key-value for misc settings
    Set(key string, value []byte) error
    Get(key string) ([]byte, error)
}

type Fabric struct {
    ID              uint64
    Label           string
    RootCertPEM     []byte
    ICACertPEM      []byte
    PrivateKeyPEM   []byte
    VendorID        uint16
    FabricIndex     uint8
    CreatedAt       time.Time
}



type Node struct {
    ID              uint64
    FabricID        uint64
    Name

            string          // from BasicInformation
    VendorID        uint16
    ProductID       uint16
    Endpoints       []Endpoint      // cached device structure
    LastAddress     string          // IP:port for reconnection
    LastSeen        time.Time
}

type Endpoint struct {
    ID       uint16
    DeviceTypes []DeviceType
    Clusters    []ClusterRef
}

type DeviceType struct {
    ID       uint32
    Revision uint16
}

type ClusterRef struct {
    ID         uint32
    Name       string
    Side       string // "server" or "client"
}
```

**Key design decisions:**
- Use `go.etcd.io/bbolt` (BoltDB) — single-file embedded DB, zero config, works great for CLI tools
- Storage location: `~/.config/matter-cli/` (respect `XDG_CONFIG_HOME`)
- In-memory implementation for all tests — never hit disk in unit tests
- Fabric private keys stored encrypted at rest (use a user-provided passphrase or OS keychain)

**Completion criteria:**
- [x] All CRUD operations work for fabrics, nodes, resumption info
- [x] BoltDB implementation passes the same test suite as in-memory
- [x] Concurrent access is safe
- [x] Storage directory is created automatically on first use

**Dependencies:** None (data types only)

**Blocked by:** Nothing — start immediately, parallel with Agents 1-3

---

### Agent 9: CLI & Interactive Mode

**Scope:** `cli/`, `cmd/matter-cli/`

**Responsibility:** All user-facing CLI commands, interactive REPL, output formatting, shell completions.

**Files to produce:**
- `cmd/matter-cli/main.go` — Entrypoint
- `cli/root.go` — Root command, global flags, Viper config loading
- `cli/commission.go` — `matter-cli commission` subcommands
- `cli/discover.go` — `matter-cli discover` subcommands
- `cli/device.go` — `matter-cli device ls`, `matter-cli device inspect`
- `cli/cluster.go` — `matter-cli cluster read|write|invoke|subscribe`
- `cli/interactive.go` — `matter-cli interactive` REPL
- `cli/config.go` — `matter-cli config set|get|list`
- `cli/payload.go` — `matter-cli payload parse|generate`
- `cli/output/formatter.go` — Output format switching
- `cli/output/table.go` — Lipgloss-styled table output
- `cli/output/json.go` — JSON output
- `cli/output/tree.go` — Tree-style output for device inspection
- `cli/completion/completer.go` — Dynamic completion logic

**External dependencies:**
```
github.com/spf13/cobra
github.com/spf13/viper
github.com/charmbracelet/huh
github.com/charmbracelet/lipgloss
github.com/charmbracelet/bubbletea
github.com/charmbracelet/bubbles
github.com/muesli/termenv
```

**Command tree with examples:**

```
matter-cli
│
├── commission
│   ├── code        matter-cli commission code "MT:Y3.13OTB00KA0648G00" --node 1
│   ├── ip          matter-cli commission ip 192.168.1.100 --setup-pin 12345678 --node 2
│   └── forget      matter-cli commission forget --node 1
│
├── discover
│   ├── commissionable   matter-cli discover commissionable
│   └── operational      matter-cli discover operational
│
├── device
│   ├── ls          matter-cli device ls
│   ├── inspect     matter-cli device inspect --node 1
│   └── alias       matter-cli device alias --node 1 --name "Kitchen Light"
│
├── cluster
│   ├── read        matter-cli cluster read --node 1 -e 1 --cluster on-off --attribute on-off
│   ├── write       matter-cli cluster write --node 1 -e 1 --cluster on-off --attribute on-time --value 300
│   ├── invoke      matter-cli cluster invoke --node 1 -e 1 --cluster on-off --command toggle
│   └── subscribe   matter-cli cluster subscribe --node 1 -e 1 --cluster on-off --attribute on-off --min 1 --max 10
│
│   # Shorthand cluster commands (aliases auto-registered from cluster registry):
├── on-off
│   ├── on          matter-cli on-off on --node 1 -e 1
│   ├── off         matter-cli on-off off --node 1 -e 1
│   ├── toggle      matter-cli on-off toggle --node 1 -e 1
│   └── read        matter-cli on-off read on-off --node 1 -e 1
│
├── payload
│   ├── parse       matter-cli payload parse "MT:Y3.13OTB00KA0648G00"
│   └── generate    matter-cli payload generate --vid 0xFFF1 --pid 0x8000 --passcode 12345678
│
├── config
│   ├── set         matter-cli config set default-fabric my-fabric
│   ├── get         matter-cli config get default-fabric
│   └── list        matter-cli config list
│
├── interactive     matter-cli interactive
│
└── version         matter-cli version
```

**`device ls` output example:**
```
$ matter-cli device ls
 NODE   NAME            VENDOR        PRODUCT            ENDPOINTS  LAST SEEN       
 1      Kitchen Light   Espressif     ESP32-C3-Light     2          2 minutes ago   
 2      Front Door      Nuki          Smart Lock 4.0     3          15 minutes ago  
 3      Living Room     Eve           Eve Thermo         2          1 hour ago      
```

**`device inspect` output example:**
```
$ matter-cli device inspect --node 1
Kitchen Light (Node 1)
  Vendor:  Espressif (0xFFF1)
  Product: ESP32-C3-Light (0x8000)
  Address: 192.168.1.42:5540
  
  Endpoints:
  ├── Endpoint 0 (Root Node)
  │   ├── Descriptor (0x001D)
  │   ├── Access Control (0x001F)
  │   ├── Basic Information (0x0028)
  │   ├── General Commissioning (0x0030)
  │   ├── Network Commissioning (0x0031)
  │   ├── General Diagnostics (0x0033)
  │   └── Operational Credentials (0x003E)
  │
  └── Endpoint 1 (Dimmable Light)
      ├── Descriptor (0x001D)
      ├── Identify (0x0003)
      ├── On/Off (0x0006)
      │   ├── on-off: true
      │   ├── global-scene-control: true
      │   ├── on-time: 0
      │   └── off-wait-time: 0
      ├── Level Control (0x0008)
      │   ├── current-level: 254
      │   ├── min-level: 1
      │   └── max-level: 254
      └── Color Control (0x0300)
          ├── color-temperature-mireds: 250
          └── color-mode: 2 (ColorTemperature)
```

**Interactive mode design:**

Use `bubbletea` for a full TUI REPL with:
- Prompt: `matter> ` (or `matter/node-1/ep-1> ` when context is set)
- Tab completion via `bubbles/textinput` with suggestions from the cluster registry
- Command syntax in interactive mode:
  ```
  matter> use node 1 endpoint 1
  matter/node-1/ep-1> on-off toggle
  matter/node-1/ep-1> on-off read on-off
  ✓ on-off: true
  matter/node-1/ep-1> level-control read current-level
  ✓ current-level: 254
  matter/node-1/ep-1> subscribe on-off on-off --min 1 --max 10
  ◉ Subscribed. Listening...
  [14:32:01] on-off: false
  [14:32:03] on-off: true
  ^C
  matter/node-1/ep-1> device inspect
  [tree output]
  matter/node-1/ep-1> exit
  ```

**Dynamic completions for Cobra (bash/zsh/fish/powershell):**
```go
// In commission.go
cmd := &cobra.Command{
    Use:   "code [setup-code]",
    Short: "Commission a device using a QR or manual pairing code",
    ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
        // No completion for setup codes
        return nil, cobra.ShellCompDirectiveNoFileComp
    },
}

// In cluster.go — dynamic cluster name completion
clusterFlag.RegisterFlagCompletionFunc("cluster", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
    results := clusters.Registry.SearchClusters(toComplete)
    names := make([]string, len(results))
    for i, c := range results {
        names[i] = fmt.Sprintf("%s\t%s (0x%04X)", c.Name, c.DisplayName, c.ID)
    }
    return names, cobra.ShellCompDirectiveNoFileComp
})

// In cluster.go — dynamic attribute name completion (depends on --cluster value)
attrFlag.RegisterFlagCompletionFunc("attribute", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
    clusterName

, _ := cmd.Flags().GetString("cluster")
    cluster, ok := clusters.Registry.ClusterByName(clusterName)
    if !ok {
        return nil, cobra.ShellCompDirectiveError
    }
    results := clusters.Registry.SearchAttributes(cluster.ID, toComplete)
    names := make([]string, len(results))
    for i, a := range results {
        names[i] = fmt.Sprintf("%s\t%s (0x%04X)", a.Name, a.DisplayName, a.ID)
    }
    return names, cobra.ShellCompDirectiveNoFileComp
})

// In device.go — dynamic node completion from store
nodeFlag.RegisterFlagCompletionFunc("node", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
    nodes, _ := store.ListNodes(currentFabricID)
    names := make([]string, len(nodes))
    for i, n := range nodes {
        names[i] = fmt.Sprintf("%d\t%s", n.ID, n.Name)
    }
    return names, cobra.ShellCompDirectiveNoFileComp
})
```

**Key design decisions:**
- All commands that interact with a device accept `--node` / `-n` (required) and `--endpoint` / `-e` (often required)
- Default fabric is configurable: `matter-cli config set default-fabric <name>`
- Output formatter is injected — commands return data

 structs, the formatter decides how to render them
- `--format` / `-f` flag: `table` (default in TTY), `json` (default in pipe), `yaml`
- Errors go to stderr with styled formatting; data goes to stdout (clean for piping)
- `--verbose` / `-v` flag enables debug logging to stderr
- Interactive mode reuses the same command execution engine — no separate code paths

**Completion criteria:**
- [x] All commands in the tree above are implemented with `--help` and examples
- [x] Shell completions work for bash, zsh, fish
- [x] Dynamic completions work for cluster names, attribute names, node IDs
- [x] `device ls` shows commissioned devices in a clean table
- [x] `device inspect` shows full device tree with attribute values
- [x] Interactive mode has working tab completion and history
- [x] `--format json` produces machine-parseable output
- [x] Colors respect `NO_COLOR` and non-TTY detection

**Dependencies:** Agent 5 (Interaction Model), Agent 6 (Cluster Registry), Agent 7 (Commissioning/Discovery), Agent 8 (Store

)

**Blocked by:** Agent 6 (Registry — needed for completions), Agent 8 (Store — needed for `device ls`)

---

## Execution Order & Parallelism

```
Week 1-2:    [Agent 1: TLV] ───────────────────┐
             [Agent 2: Crypto] ────────────────┤
             [Agent 8: Store] ─────────────────┤
                                               │
Week 2-3:    [Agent 3: Protocol] ◄─────────────┤ (needs TLV + Crypto for encrypted codec)
             [Agent 6: Codegen+Registry] ◄─────┘ (needs TLV tag format finalized)
                                               │
Week 3-5:    [Agent 4: PASE/CASE] ◄────────────┤ (needs Crypto + Protocol)
             [Agent 5: IM] ◄───────────────────┤ (needs TLV + Protocol)
                                               │
Week 5-7:    [Agent 7: Commission+Discovery] ◄─┤

 (needs Sessions + IM + Clusters)
                                               │
Week 3-8:    [Agent 9: CLI] ◄──────────────────┘ (starts with scaffold in week 3,
              │                                    wires up features as they land)
              │
              ├── Week 3-4: Command structure, output formatters, config
              ├── Week 4-5: discover, payload commands (no device interaction needed)
              ├── Week 5-6: device ls/inspect (needs Store + Registry)
              ├── Week 6-7: cluster read/write/invoke (needs IM)
              ├── Week 7-8: commission command (needs Commissioning)
              └── Week 8-9: interactive REPL (needs everything)
```

## Integration Milestones

Each milestone is a working end-to-end test. Celebrate these. 🎉

| # | Milestone | What works |

 Validates |
|---|---|---|---|
| M1 | **TLV round-trip** |

 Encode → decode → equal for all types | Agent 1 |
| M2 | **SPAKE2+ handshake** | Two in-process parties derive matching keys | Agent 2 |
| M3 | **UDP echo** | Send/receive Matter-framed messages over localhost | Agent 3 |
| M4 | **PASE over loopback** | Establish encrypted session between two local instances | Agents 1-4 |
| M5 | **Read from real device** | `matter-cli cluster read` returns data from an ESP32 | Agents 1-5 |
| M6 | **Toggle a light** | `matter-cli on-off toggle --node 1 -e 1` works | Agents 1-6 |
| M7 | **Commission a device** | `matter-cli commission code "MT:..."` does full commissioning | Agents 1-8 |
| M8 | **Device inspection** | `matter-cli device inspect --node 1` shows full device tree | All agents |
| M9 | **Interactive REPL** | Tab-complete cluster names, invoke commands interactively | All agents |

## Test Device Setup

Every agent that touches the network should be tested against a real Matter device. Use:

1. **matter-js** example device.

  You can spin up a Matter device simulator using matter-js. (https://github.com/matter-js/matter.js)
  A simple test device can be started with those commands in the `examples/matter-js-test-device` directory:
  ```
  node --enable-source-maps dist/DeviceNode.js
  ```
  This will run a virtual On/Off light device that you can interact with using your CLI tool.

2. **chip-tool** as reference — capture packet traces to validate your implementation

## Coding Standards

### File header
Every file starts with:
```go
// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0
```

### Error handling pattern
```go
// Good: wrap with context
if err := session.Establish(ctx);

 err != nil {
    return fmt.Errorf("establishing PASE session: %w", err)
}

// Good: sentinel errors for expected conditions
var ErrDeviceNotFound = errors.New("device not found")
var ErrSessionExpired = errors.New("session expired")
```

### Logging
Use `log/slog` (Go 1.21+ structured logging):
```go
slog.Debug("sending read request",
    "node", nodeID,
    "endpoint", endpoint,
    "cluster", clusterName,
    "attribute", attrName,
)
```

### Test pattern
```go
func TestTLVEncodeUint(t *testing.T) {
    tests := []struct {
        name     string
        tag      tlv.Tag
        value    uint64
        expected []byte
    }{
        {"uint8 zero", tlv.ContextTag(0), 0, []byte{0x24, 0x00, 0x00}},
        {"uint8 max", tlv.ContextTag(1), 255, []byte{0x24, 0x01, 0xFF}},
        {"uint16", tlv.ContextTag(2), 256, []byte{0x25, 0x02, 0x00, 0x01}},
        // ...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var buf bytes.Buffer
            w

 := tlv.NewWriter(&buf)
            err := w.PutUint(tt.tag, tt.value)
            require.NoError(t, err)
            assert.Equal(t, tt.expected, buf.Bytes())
        })
    }
}
```

### Dependencies (go.mod)
```go
module github.com/<org>/matter-cli

go 1.23

require (
    github.com/spf13/cobra v1.8+
    github.com/spf13/viper v1.19+
    github.com/charmbracelet/bubbletea v1.2+
    github.com/charmbracelet/bubbles v0.20+
    github.com/charmbracelet/huh v0.6+
    github.com/charmbracelet/lipgloss v1.0+
    github.com/muesli/termenv v0.15+
    github.com/grandcat/zeroconf v1.0+     // mDNS
    go.etcd.io/bbolt v1.3+                 // embedded DB
    golang.org/x/crypto v0.31+             // HKDF, PBKDF2
    github.com/stretchr/testify v1.9+      // test assertions
)
```

## Git workflow

Agents must treat commits as part of the development process, not an afterthought. When starting a new feature or command, agents should create a new branch to develop the feature or command. When the feature or command is complete, agents should merge the branch into the main branch using a pull request.

### When to commit

Agents should **suggest committing** (or commit directly if the user has granted permission) at these points:

* **After a feature is confirmed working** — tests pass, the user is satisfied with the result. This is the most important commit point.
* **After fixing a bug** — once the fix is verified with a test.
* **Before starting a risky refactor** — so there is a clean rollback point.
* **After a meaningful intermediate milestone** — e.g., "parser done and tested, index not started yet." Don't wait until everything is finished if the work is large.
* **After updating docs, config, or CI** — these are self-contained changes worth capturing.

When in doubt, commit more often rather than less. Small, well-described commits are cheap and easy to review or revert.

### Commit message format

Use short, imperative-mood subject lines. A body is optional but encouraged for non-trivial changes.

```
<type>: <concise summary>

Optional longer explanation of what changed and why.
```

Types (lowercase):

* `feat` — new feature or command
* `fix` — bug fix
* `refactor` — restructuring without behavior change
* `test` — adding or updating tests only
* `docs` — documentation, README, AGENTS.md
* `chore` — CI, Makefile, tooling, dependency updates
* `style` — formatting, linting fixes (no logic change)

Examples:

```
feat: add completion command with --install flag
fix: use forward slashes in zsh post-install hint on Windows
test: add cross-platform tests for completion install paths
chore: add Makefile and Zed tasks
docs: rewrite versioning section in AGENTS.md
```

### What to commit

* **Do** commit source code, tests, config, documentation, CI, Makefile, and Zed tasks.
* **Do not** commit build artifacts (`bin/`), coverage files (`coverage.out`, `coverage.html`), or OS junk (`.DS_Store`). These are already in `.gitignore`.

### Pre-commit checks

Before committing, agents must verify:

1. `make lint` passes (or at minimum `go vet ./...` and `gofmt -l .` reports no files).
2. `make test` passes.
3. No unrelated changes are staged — keep commits focused.

If a commit includes a new feature, the commit should include the tests for that feature.

## Notes for AI Agents

1. **When unsure about spec behavior:** Look at the C++ SDK implementation in `connectedhomeip/src/` as the reference. It IS the spec in practice.

2. **When writing crypto code:** ALWAYS validate against known test vectors. Never trust that "it looks right." Run vectors from both the IETF RFCs and the Matter SDK test files.

3. **When implementing protocol messages:** Capture real packets using chip-tool with `--trace_decode 1` or Wireshark with the Matter dissector. Compare your encoded bytes against the capture.

4. **When generating cluster code:** The single source of truth is `connectedhomeip/src/controller/data_model/controller-clusters.matter`. Parse THIS file.

5. **When implementing CLI commands:** Look at `gh` (GitHub CLI) source code for patterns:

 https://github.com/cli/cli — specifically how they handle output formatting, config, and interactive prompts.

6. **Package boundaries are contracts.** Each agent owns their package's public API. If you need to change another agent's API, document the change needed and coordinate — do not reach into their internals.

7. **Every public function and type must have a godoc comment.** No exceptions.
