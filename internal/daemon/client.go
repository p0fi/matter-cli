// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/p0fi/matter-cli/internal/store"
)

// Client connects to a running daemon over its Unix domain socket and sends
// requests. Each method opens a new connection, sends one JSON request, reads
// one JSON response, and closes the connection.
type Client struct {
	socketPath string
}

// NewClient creates a daemon client that will connect to the given socket path.
// If socketPath is empty, the default SocketPath() is used.
func NewClient(socketPath string) *Client {
	if socketPath == "" {
		socketPath = SocketPath()
	}
	return &Client{socketPath: socketPath}
}

// IsRunning checks whether the daemon is currently running by attempting to
// connect to its Unix socket and sending a ping. Returns true only if the
// daemon responds successfully.
func (c *Client) IsRunning() bool {
	resp, err := c.send(Request{Type: "ping"})
	if err != nil {
		return false
	}
	return resp.OK
}

// Ping sends a ping request and returns the daemon status.
func (c *Client) Ping() (*StatusResp, error) {
	resp, err := c.send(Request{Type: "ping"})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon: ping failed: %s", resp.Error)
	}
	return resp.Status, nil
}

// Status requests the full daemon status including cached sessions.
func (c *Client) Status() (*StatusResp, error) {
	resp, err := c.send(Request{Type: "status"})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon: status failed: %s", resp.Error)
	}
	return resp.Status, nil
}

// ListNodes returns all commissioned nodes for the given fabric. If fabricID
// is 0, the daemon uses its configured default fabric. This is called by shell
// completion subprocesses so they can obtain node data without trying to open
// the BoltDB file that the daemon holds locked.
func (c *Client) ListNodes(fabricID uint64) ([]*store.Node, error) {
	resp, err := c.send(Request{
		Type:     "list-nodes",
		FabricID: fabricID,
	})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon: list-nodes: %s", resp.Error)
	}
	if resp.Nodes == nil {
		return nil, nil
	}
	return resp.Nodes.Nodes, nil
}

// GetFabric returns the fabric record for the given fabric ID. If fabricID is
// 0, the daemon uses its configured default. Used by CLI commands that display
// fabric information while the daemon holds the BoltDB lock.
func (c *Client) GetFabric(fabricID uint64) (*store.Fabric, error) {
	resp, err := c.send(Request{
		Type:     "get-fabric",
		FabricID: fabricID,
	})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon: get-fabric: %s", resp.Error)
	}
	if resp.Fabric == nil {
		return nil, fmt.Errorf("daemon: get-fabric: empty response")
	}
	return resp.Fabric.Fabric, nil
}

// SaveNode persists a node record via the daemon. Used by CLI commands (e.g.
// device alias) that need to write node data while the daemon holds the lock.
func (c *Client) SaveNode(fabricID uint64, node *store.Node) error {
	resp, err := c.send(Request{
		Type:     "save-node",
		FabricID: fabricID,
		SaveNode: node,
	})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon: save-node: %s", resp.Error)
	}
	return nil
}

// Shutdown asks the daemon to shut down gracefully.
func (c *Client) Shutdown() error {
	resp, err := c.send(Request{Type: "shutdown"})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("daemon: shutdown failed: %s", resp.Error)
	}
	return nil
}

// Invoke sends an invoke request through the daemon, which uses its cached
// CASE session (or establishes a new one) to execute the command on the device.
func (c *Client) Invoke(nodeID, fabricID uint64, inv *InvokeReq) (*InvokeResp, error) {
	resp, err := c.send(Request{
		Type:     "invoke",
		NodeID:   nodeID,
		FabricID: fabricID,
		Invoke:   inv,
	})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon: invoke: %s", resp.Error)
	}
	return resp.Invoke, nil
}

// Read sends a read request through the daemon.
func (c *Client) Read(nodeID, fabricID uint64, rd *ReadReq) (*ReadResp, error) {
	resp, err := c.send(Request{
		Type:     "read",
		NodeID:   nodeID,
		FabricID: fabricID,
		Read:     rd,
	})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon: read: %s", resp.Error)
	}
	return resp.Read, nil
}

// Write sends a write request through the daemon.
func (c *Client) Write(nodeID, fabricID uint64, wr *WriteReq) (*WriteResp, error) {
	resp, err := c.send(Request{
		Type:     "write",
		NodeID:   nodeID,
		FabricID: fabricID,
		Write:    wr,
	})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon: write: %s", resp.Error)
	}
	return resp.Write, nil
}

// send opens a connection to the daemon, sends a JSON request, reads the JSON
// response, and closes the connection.
func (c *Client) send(req Request) (*Response, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("daemon: connecting to %s: %w", c.socketPath, err)
	}
	defer conn.Close()

	// Set an overall deadline for the exchange. Most operations complete
	// quickly, but CASE establishment on a constrained device can be slow.
	conn.SetDeadline(time.Now().Add(60 * time.Second))

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("daemon: marshaling request: %w", err)
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("daemon: sending request: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("daemon: reading response: %w", err)
		}
		return nil, fmt.Errorf("daemon: connection closed without response")
	}

	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("daemon: unmarshaling response: %w", err)
	}
	return &resp, nil
}

// IsProcessRunning checks whether the daemon PID file exists and the
// referenced process is still alive. This is a cheaper check than IsRunning
// when you just need to know if the daemon process exists (without verifying
// socket connectivity).
func IsProcessRunning() bool {
	pidPath := PidPath()
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds. Send signal 0 to check liveness.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
