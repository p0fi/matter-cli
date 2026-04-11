# AGENTS.md — Matter CLI

## Project Overview

**Goal:** Build `matter-cli`, a pure Go Matter controller CLI with modern ergonomics, inspired by GitHub CLI (`gh`). No C++ dependencies, no ZAP tooling. Single static binary.

## Matter Specification Source for Implementation
You can find the official Matter specification source files in `../connectedhomeip-spec/src/`. Those are the canonical references for all protocol details, TLV encoding rules, cryptographic algorithms, and cluster definitions. The C++ Matter SDK is the reference implementation, but the spec source files are the ultimate authority. You can find the source files for the C++ Matter SDK in `../connectedhomeip/`.

## Design Principles

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
if err := session.Establish(ctx); err != nil {
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
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var buf bytes.Buffer
            w := tlv.NewWriter(&buf)
            err := w.PutUint(tt.tag, tt.value)
            require.NoError(t, err)
            assert.Equal(t, tt.expected, buf.Bytes())
        })
    }
}
```

---

## Build System

This project uses **[mise-en-place](https://mise.jdx.dev/)** (`mise`) to manage tool versions and define build tasks. All agents **must** activate mise before running any build, test, or lint commands so that they use the exact Go and tool versions pinned in `mise.toml`.

### Activating mise

Before running any commands, activate mise in the current shell:

```bash
eval "$(mise activate bash)"   # or: eval "$(mise activate zsh)"
```

Alternatively, prefix every command with `mise exec --`:

```bash
mise exec -- go build ./...
```

### Available tasks

Use `mise run <task>` instead of invoking tools directly:

| Task | Description |
|------|-------------|
| `mise run build` | Build the `matter` binary into `bin/` with version/commit/date ldflags |
| `mise run install` | Install the `matter` binary via `go install` with ldflags |
| `mise run build-install` | Build and install (runs both tasks) |
| `mise run test` | Run tests with race detector (`-race -count=1`) |
| `mise run test-cover` | Run tests with coverage, generate HTML report |
| `mise run vet` | Run `go vet ./...` |
| `mise run lint` | Run `golangci-lint` (includes vet) |
| `mise run fmt` | Format Go source files with `gofmt` |
| `mise run clean` | Remove build artifacts (`bin/`, coverage files) |

### Rules for agents

1. **Always use `mise run`** for build, test, lint, and format tasks — never call `go build`, `go test`, `golangci-lint`, or `gofmt` directly. The mise tasks include required ldflags, dependencies, and consistent tool versions.
2. **Never assume Go or tooling is on `$PATH`** without mise activation. The project pins specific versions in `mise.toml`; using a different version can cause subtle breakage.
3. **When adding a new build step or tool**, add it to `mise.toml` rather than a Makefile or shell script.

---

## Git workflow

Agents must treat commits as part of the development process, not an afterthought. When starting a new feature or command, agents should create a new branch to develop the feature or command. When the feature or command is complete, agents should merge the branch into the main branch using a pull request.

### When to commit

Agents should **suggest committing** (or commit directly if the user has granted permission) at these points:

* **After a feature is confirmed working** — tests pass, the user is satisfied with the result.
* **After fixing a bug** — once the fix is verified with a test and confirmed by the user.
* **Before starting a risky refactor** — so there is a clean rollback point.
* **After a meaningful intermediate milestone** — e.g., "parser done and tested, index not started yet."
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

### What to commit

* **Do** commit source code, tests, config, documentation, CI, and task definitions.
* **Do not** commit build artifacts (`bin/`), coverage files (`coverage.out`, `coverage.html`), or OS junk (`.DS_Store`). These are already in `.gitignore`.

### Pre-commit checks

Before committing, agents must verify:

1. `mise run lint` passes (or at minimum `go vet ./...` and `gofmt -l .` reports no files).
2. `mise run test` passes.
3. No unrelated changes are staged — keep commits focused.

If a commit includes a new feature, the commit should include the tests for that feature.

---

## Notes for AI Agents

1. **When unsure about spec behavior:** Look at the C++ SDK implementation in `connectedhomeip/src/` as the reference. It IS the spec in practice.

2. **When writing crypto code:** ALWAYS validate against known test vectors. Never trust that "it looks right." Run vectors from both the IETF RFCs and the Matter SDK test files.

3. **When implementing protocol messages:** Capture real packets using chip-tool with `--trace_decode 1` or Wireshark with the Matter dissector. Compare your encoded bytes against the capture.

4. **When generating cluster code:** The single source of truth is `connectedhomeip/src/controller/data_model/controller-clusters.matter`. Parse THIS file.

5. **When implementing CLI commands:** Look at `gh` (GitHub CLI) source code for patterns: https://github.com/cli/cli — specifically how they handle output formatting, config, and interactive prompts.

6. **Package boundaries are contracts.** Each package owns its public API. If you need to change another package's API, document the change needed and coordinate — do not reach into internals.

7. **Every public function and type must have a godoc comment.** No exceptions.

8. **CRITICAL — Never call `openStore()` or `store.NewBoltStore()` directly in CLI commands.** The session daemon (`-K` flag) holds an exclusive BoltDB `flock` for its entire lifetime. Any direct `bolt.Open()` call from the CLI will hang forever while the daemon is running. **Always use the daemon-aware helpers defined in `cli/device.go` instead:**

   | Need | Helper to use |
   |------|---------------|
   | List all nodes | `listNodesForCompletion(fabricID)` |
   | Get a single node | `getNodeForCompletion(fabricID, nodeID)` |
   | Get fabric info | `getFabric(fabricID)` |
   | Save / update a node | `saveNode(fabricID, node)` |

   These helpers call `daemon.NewClient("").IsRunning()` first. When the daemon is running they proxy the request through its Unix socket. When no daemon is running they open the DB directly.

   **Shell completion functions** (in `cli/completion/completer.go`) have an equivalent `listNodes(fabricID)` helper — use it instead of opening the store directly.

   **Commands that need exclusive write access** (e.g. commissioning) cannot be proxied through the daemon and must refuse early when the daemon is running:
   ```go
   if daemon.NewClient("").IsRunning() {
       return fmt.Errorf(
           "a session daemon is running and holds the database lock\n" +
               "Stop it first with: matter session stop")
   }
   ```

   **Adding a new store operation not covered by the helpers above:** extend the daemon protocol rather than bypassing it:
   1. Add request/response types to `internal/daemon/protocol.go`
   2. Add a handler (`handleXxx`) in `internal/daemon/server.go`
   3. Add a client method in `internal/daemon/client.go`
   4. Add a daemon-aware helper in `cli/device.go` that checks `IsRunning()` and falls back to `openStore()` when no daemon is present
