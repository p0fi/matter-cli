# CLAUDE.md — Matter CLI

## Project Overview

**Goal:** Build `matter-cli`, a pure Go Matter controller CLI with modern ergonomics, inspired by GitHub CLI (`gh`). No C++ dependencies, no ZAP tooling. Single static binary.

## Matter Specification Source for Implementation

You can find the official Matter specification source files in `../connectedhomeip-spec/src/`. Those are the canonical references for all protocol details, TLV encoding rules, cryptographic algorithms, and cluster definitions. The C++ Matter SDK is the reference implementation, but the spec source files are the ultimate authority. You can find the source files for the C++ Matter SDK in `../connectedhomeip/`.

## Design Principles

1. **Idiomatic Go** — `io.Reader`/`io.Writer` interfaces, `fmt.Errorf("doing X: %w", err)` wrapping, no `panic`, `context.Context` for blocking ops, `internal/` for non-public packages, small interfaces defined by the consumer, useful zero values.
2. **Testability** — >80 % coverage, table-driven `t.Run` subtests, no global state, dependency injection, `//go:build integration` tags for network tests, crypto validated against spec test vectors.
3. **CLI Ergonomics** — `--format json|table|yaml` (default: `table` for TTY, `json` for pipes), short flags everywhere (`-n`, `-e`, `-f`), `NO_COLOR` respected, errors to stderr with next-step suggestions. Libraries: cobra, viper, charmbracelet (huh, lipgloss, bubbletea, bubbles) — see `go.mod`.
4. **No ZAP** — Cluster definitions generated from `.matter` IDL files via `internal/codegen/`, each cluster is a self-contained Go package.
5. **Human-Readable Everything** — All IDs have string name mappings, CLI accepts names or hex (`--cluster on-off` or `--cluster 0x0006`).
6. **Reference Implementations** — C++: [connectedhomeip](https://github.com/project-chip/connectedhomeip), Go: [gomat](https://github.com/tom-code/gomat), JS/TS: [matter.js](https://github.com/matter-js/matter.js).

---

## CLI Usage

> **Keep this section in sync.** If you add, rename, or remove a command or flag, update the examples below to match.

The binary is called `matter`. Targets use `@node/endpoint` syntax with a numeric node ID (e.g. `@1`, `@1/1`, `@42/2`). Device aliases are not supported in the target syntax.

### Core commands

```bash
# Discover devices on the network
matter discover commissionable
matter discover ble

# Commission a new device
matter commission code MT:Y3.13OTB00KA0648G00
matter commission ip 192.168.1.42 --setup-code 34970112332

# Open a fresh commissioning window on an already-commissioned device so a
# second ecosystem (Apple Home, Google Home, Alexa, ...) can commission it
# without a factory reset. Prints a QR + manual pairing code.
matter @1 commission open-window
matter @1 commission open-window --timeout 5m
matter @1 commission open-window --passcode 20202021 --discriminator 3840
matter @1 commission open-window --basic         # Basic Commissioning Method
matter @1 commission close-window                # revoke an open window

# List commissioned devices and inspect them
matter fabric ls
matter @1 tree                  # show endpoints & clusters
matter @1 tree -L 4             # full tree including attribute values
matter fabric reset             # remove all devices locally (interactive prompt)
matter fabric reset --yes       # skip confirmation (for scripts/CI)

# Rename a commissioned device (updates local store AND NodeLabel on the device)
matter rename @1 "Kitchen Light"
matter rename @1 "Kitchen Light" --local   # don't touch the device (offline)
matter rename @1 --reset                   # restore name from ProductName

# Remove a commissioned device
matter decommission @1          # proper: sends RemoveFabric over CASE, then deletes locally
matter decommission @1 --force  # delete locally even if the device is unreachable
matter fabric remove @1         # local-only: device is NOT notified (use when device is gone)

# Set a sticky default target (node/endpoint)
matter use @1/1
matter use --show
matter use --clear
```

### Code parsing & generation

```bash
# Parse a QR code or manual pairing code
matter code parse "MT:Y3.13OTB00KA0648G00"
matter code parse "34970112332"

# Generate a QR code and manual pairing code from parameters
matter code generate --vid 0xFFF1 --pid 0x8000 --passcode 12345678 --discriminator 3840
```

### Cluster interaction

```bash
# Generic cluster commands
matter cluster read  --cluster on-off --attribute on-off @1/1
matter cluster write --cluster level-control --attribute current-level 128 @1/1
matter cluster invoke --cluster on-off --command toggle @1/1

# Shorthand cluster commands (same thing, fewer keystrokes)
matter OnOff Toggle @1/1
matter OnOff read OnOff @1/1
matter LevelControl write CurrentLevel 128 @1/1
```

### Session daemon & interactive mode

```bash
# Keep CASE sessions alive across invocations (avoids re-handshake)
matter -K 30m OnOff Toggle @1/1   # inline: start/reuse daemon with 30 min idle timeout
matter session status              # check daemon state
matter session stop                # stop it

# Interactive REPL (alias: matter i)
matter interactive
```

### Global flags

| Flag | Short | Description |
|------|-------|-------------|
| `--format json\|table\|yaml` | `-f` | Output format (default: `table` for TTY, `json` for pipes) |
| `--keep-alive <duration>` | `-K` | Start/reuse session daemon with idle timeout |
| `--verbose` | `-v` | Enable debug logging |

---

## Coding Standards

See **[`docs/CODING_STANDARDS.md`](docs/CODING_STANDARDS.md)** for file headers, error handling, logging, and test patterns.

Key rules inline: every file gets the Apache-2.0 header, errors wrap with `fmt.Errorf("doing X: %w", err)`, logging uses `log/slog`, every public symbol gets a godoc comment.

---

## Build System

This project uses **[mise](https://mise.jdx.dev/)** to pin tool versions and define tasks. **Always use `mise run <task>`** — never call `go build`, `go test`, `golangci-lint`, or `gofmt` directly.

Activate first: `eval "$(mise activate bash)"` — or prefix commands with `mise exec --`.

Common tasks: `mise run build`, `mise run test`, `mise run lint`, `mise run fmt`. Run `mise tasks` to see the full list.

**Agent rules:**
1. Always use `mise run` — the tasks include required ldflags, race flags, and consistent tool versions.
2. Never assume Go or tooling is on `$PATH` without mise activation.
3. New build steps go into `mise.toml`, not Makefiles or shell scripts.

---

## Git Workflow

See **[`docs/GIT_WORKFLOW.md`](docs/GIT_WORKFLOW.md)** for full commit rules, message format, and pre-commit checks.

Key rules inline: branch per feature, `mise run lint` + `mise run test` before committing, imperative-mood `<type>: <summary>` messages (`feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `style`). Commit often at meaningful milestones.

---

## Notes for AI Agents

1. **Spec behavior unclear?** Check the C++ SDK in `connectedhomeip/src/` — it IS the spec in practice.
2. **Crypto code** — ALWAYS validate against known test vectors from IETF RFCs and Matter SDK test files.
3. **Protocol messages** — Compare your encoded bytes against real captures (`chip-tool --trace_decode 1` or Wireshark).
4. **Cluster codegen** — The single source of truth is `connectedhomeip/src/controller/data_model/controller-clusters.matter`.
5. **CLI command patterns** — Follow [GitHub CLI](https://github.com/cli/cli) for output formatting, config, and prompts.
6. **Package boundaries are contracts** — Don't reach into another package's internals; coordinate API changes.
7. **Every public function and type must have a godoc comment.** No exceptions.
8. **Daemon-aware store access** — See **[`docs/DAEMON_STORE.md`](docs/DAEMON_STORE.md)**. Never call `openStore()` or `store.NewBoltStore()` directly in CLI commands; use the helpers in `cli/device.go`.
9. **BLE commissioning network credentials** — See **[`docs/BLE_COMMISSIONING_NETWORK.md`](docs/BLE_COMMISSIONING_NETWORK.md)**. When commissioning over BLE, use the WiFi credentials defined there (`tomkat-iot` / `soviets-ferry-dork`).


## Review

Codex will review all code changes once done with implementation!
