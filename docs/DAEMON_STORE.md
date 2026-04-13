# Daemon-aware store access

**CRITICAL — Never call `openStore()` or `store.NewBoltStore()` directly in CLI commands.** The session daemon (`-K` flag) holds an exclusive BoltDB `flock` for its entire lifetime. Any direct `bolt.Open()` call from the CLI will hang forever while the daemon is running. **Always use the daemon-aware helpers defined in `cli/device.go` instead:**

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