// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/p0fi/matter-cli/internal/controller"
	"github.com/p0fi/matter-cli/internal/discovery"
	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/store"
)

// derefU16 safely dereferences a *uint16, returning 0 if nil.
func derefU16(p *uint16) uint16 {
	if p == nil {
		return 0
	}
	return *p
}

// derefU32 safely dereferences a *uint32, returning 0 if nil.
func derefU32(p *uint32) uint32 {
	if p == nil {
		return 0
	}
	return *p
}

// Server is the background session daemon. It listens on a Unix domain socket,
// caches CASE sessions to Matter nodes, and serves read/write/invoke requests
// from CLI processes without re-establishing CASE each time.
type Server struct {
	socketPath  string
	idleTimeout time.Duration
	startedAt   time.Time

	store    store.Store
	fabricID uint64

	mu       sync.Mutex
	sessions map[uint64]*cachedSession // keyed by node ID
	listener net.Listener

	idleTimer *time.Timer
	cancel    context.CancelFunc
}

// cachedSession holds a controller, interaction client, and CASE session for a
// single node.
type cachedSession struct {
	ctrl      *controller.Controller
	client    *interaction.Client
	session   *protocol.Session
	nodeID    uint64
	addr      string
	createdAt time.Time
}

// ServerConfig holds configuration for creating a daemon Server.
type ServerConfig struct {
	// SocketPath is the Unix socket path. Defaults to SocketPath() if empty.
	SocketPath string
	// IdleTimeout is how long the daemon stays alive after the last request.
	IdleTimeout time.Duration
	// Store is the persistent store for looking up nodes.
	Store store.Store
	// FabricID is the fabric to operate under. Defaults to 1 if zero.
	FabricID uint64
}

// NewServer creates a new daemon server with the given configuration.
func NewServer(cfg ServerConfig) *Server {
	if cfg.SocketPath == "" {
		cfg.SocketPath = SocketPath()
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	if cfg.FabricID == 0 {
		cfg.FabricID = 1
	}
	return &Server{
		socketPath:  cfg.SocketPath,
		idleTimeout: cfg.IdleTimeout,
		store:       cfg.Store,
		fabricID:    cfg.FabricID,
		sessions:    make(map[uint64]*cachedSession),
	}
}

// Run starts the daemon, listening for requests until the idle timeout expires
// or the context is cancelled. It blocks until shutdown is complete.
func (s *Server) Run(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	s.startedAt = time.Now()

	// Clean up stale socket if it exists.
	if err := os.Remove(s.socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("daemon: removing stale socket: %w", err)
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("daemon: listening on %s: %w", s.socketPath, err)
	}
	s.listener = ln

	// Write PID file so the CLI can check if the daemon is alive.
	if err := writePIDFile(); err != nil {
		slog.Warn("daemon: failed to write PID file", "error", err)
	}

	// Start idle timer.
	s.idleTimer = time.NewTimer(s.idleTimeout)

	slog.Info("daemon: started",
		"socket", s.socketPath,
		"idle_timeout", s.idleTimeout,
		"pid", os.Getpid(),
	)

	// Accept loop in a goroutine.
	connCh := make(chan net.Conn)
	errCh := make(chan error, 1)
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				// Check if we're shutting down.
				select {
				case <-ctx.Done():
					return
				default:
				}
				errCh <- acceptErr
				return
			}
			connCh <- conn
		}
	}()

	// Main event loop.
	for {
		select {
		case <-ctx.Done():
			s.shutdown()
			return nil

		case <-s.idleTimer.C:
			slog.Info("daemon: idle timeout expired, shutting down")
			s.shutdown()
			return nil

		case acceptErr := <-errCh:
			slog.Error("daemon: accept error", "error", acceptErr)
			s.shutdown()
			return fmt.Errorf("daemon: accept: %w", acceptErr)

		case conn := <-connCh:
			s.resetIdleTimer()
			go s.handleConn(ctx, conn)
		}
	}
}

// shutdown cleans up all resources.
func (s *Server) shutdown() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		s.listener.Close()
	}
	// Close all cached sessions.
	s.mu.Lock()
	for _, cs := range s.sessions {
		if cs.ctrl != nil {
			cs.ctrl.Close()
		}
	}
	s.sessions = make(map[uint64]*cachedSession)
	s.mu.Unlock()

	// Clean up socket and PID files.
	os.Remove(s.socketPath)
	os.Remove(PidPath())

	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}

	slog.Info("daemon: shutdown complete")
}

// resetIdleTimer resets the idle timer to the configured timeout.
func (s *Server) resetIdleTimer() {
	if s.idleTimer != nil {
		if !s.idleTimer.Stop() {
			select {
			case <-s.idleTimer.C:
			default:
			}
		}
		s.idleTimer.Reset(s.idleTimeout)
	}
}

// handleConn processes a single client connection. Each connection carries
// exactly one newline-delimited JSON request and receives one JSON response,
// then closes.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	// Set a deadline so a misbehaving client doesn't block forever.
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	if !scanner.Scan() {
		slog.Debug("daemon: client disconnected without sending request")
		return
	}

	var req Request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: fmt.Sprintf("invalid request: %v", err)})
		return
	}

	slog.Debug("daemon: received request", "type", req.Type, "node", req.NodeID)

	resp := s.handleRequest(ctx, &req)
	writeResponse(conn, resp)
}

// handleRequest dispatches a request to the appropriate handler.
func (s *Server) handleRequest(ctx context.Context, req *Request) Response {
	switch req.Type {
	case "ping":
		return Response{OK: true, Status: s.buildStatus()}

	case "status":
		return Response{OK: true, Status: s.buildStatus()}

	case "shutdown":
		// Schedule shutdown after responding.
		go func() {
			time.Sleep(50 * time.Millisecond)
			if s.cancel != nil {
				s.cancel()
			}
		}()
		return Response{OK: true}

	case "list-nodes":
		return s.handleListNodes(req)

	case "get-fabric":
		return s.handleGetFabric(req)

	case "save-node":
		return s.handleSaveNode(req)

	case "delete-node":
		return s.handleDeleteNode(req)

	case "invoke":
		return s.handleInvoke(ctx, req)

	case "read":
		return s.handleRead(ctx, req)

	case "write":
		return s.handleWrite(ctx, req)

	default:
		return Response{OK: false, Error: fmt.Sprintf("unknown request type %q", req.Type)}
	}
}

// handleListNodes serves a "list-nodes" request. This is used by shell
// completion subprocesses that cannot open the BoltDB file directly because
// this daemon process holds the exclusive file lock.
func (s *Server) handleListNodes(req *Request) Response {
	fabricID := req.FabricID
	if fabricID == 0 {
		fabricID = s.fabricID
	}
	nodes, err := s.store.ListNodes(fabricID)
	if err != nil {
		return Response{OK: false, Error: fmt.Sprintf("listing nodes: %v", err)}
	}
	return Response{OK: true, Nodes: &NodesResp{Nodes: nodes}}
}

// handleGetFabric serves a "get-fabric" request.
func (s *Server) handleGetFabric(req *Request) Response {
	fabricID := req.FabricID
	if fabricID == 0 {
		fabricID = s.fabricID
	}
	fabric, err := s.store.GetFabric(fabricID)
	if err != nil {
		return Response{OK: false, Error: fmt.Sprintf("getting fabric: %v", err)}
	}
	return Response{OK: true, Fabric: &FabricResp{Fabric: fabric}}
}

// handleSaveNode serves a "save-node" request, persisting the supplied node
// record. This allows CLI commands (e.g. device alias) to update node data
// while the daemon holds the exclusive BoltDB lock.
func (s *Server) handleSaveNode(req *Request) Response {
	if req.SaveNode == nil {
		return Response{OK: false, Error: "save-node request missing save_node field"}
	}
	fabricID := req.FabricID
	if fabricID == 0 {
		fabricID = s.fabricID
	}
	if err := s.store.SaveNode(fabricID, req.SaveNode); err != nil {
		return Response{OK: false, Error: fmt.Sprintf("saving node: %v", err)}
	}
	return Response{OK: true}
}

// handleDeleteNode serves a "delete-node" request, removing the named node
// from the store and evicting any cached session for it.
func (s *Server) handleDeleteNode(req *Request) Response {
	if req.NodeID == 0 {
		return Response{OK: false, Error: "delete-node request missing node_id"}
	}
	fabricID := req.FabricID
	if fabricID == 0 {
		fabricID = s.fabricID
	}
	if err := s.store.DeleteNode(fabricID, req.NodeID); err != nil {
		return Response{OK: false, Error: fmt.Sprintf("deleting node: %v", err)}
	}
	// Evict any cached session so stale state is not left behind.
	s.evictSession(req.NodeID)
	return Response{OK: true}
}

// getOrCreateSession returns a cached CASE session for the given node, or
// establishes a new one if none exists.
func (s *Server) getOrCreateSession(ctx context.Context, nodeID, fabricID uint64) (*interaction.Client, *protocol.Session, error) {
	if fabricID == 0 {
		fabricID = s.fabricID
	}

	s.mu.Lock()
	cs, ok := s.sessions[nodeID]
	s.mu.Unlock()

	if ok && cs.ctrl != nil {
		slog.Debug("daemon: reusing cached session", "node", nodeID)
		return cs.client, cs.session, nil
	}

	slog.Info("daemon: establishing new CASE session", "node", nodeID)

	node, err := s.store.GetNode(fabricID, nodeID)
	if err != nil {
		return nil, nil, fmt.Errorf("looking up node %d: %w", nodeID, err)
	}
	if node.LastAddress == "" {
		return nil, nil, fmt.Errorf("node %d has no known address", nodeID)
	}

	ctrl, err := controller.New(controller.Config{
		Store:    s.store,
		FabricID: fabricID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("creating controller: %w", err)
	}

	session, err := ctrl.ConnectCASE(ctx, node.LastAddress, nodeID)
	if err != nil {
		// Stored address is unreachable — attempt operational rediscovery via
		// mDNS so we can find the device's new IP (e.g. after DHCP renewal).
		slog.Info("daemon: CASE failed with stored address, attempting mDNS rediscovery",
			"node", nodeID, "addr", node.LastAddress, "err", err)
		rediscAddr, rediscErr := daemonRediscoverNode(ctx, ctrl, nodeID)
		if rediscErr != nil {
			slog.Debug("daemon: mDNS rediscovery failed", "node", nodeID, "err", rediscErr)
			ctrl.Close()
			return nil, nil, fmt.Errorf("establishing CASE session to node %d: %w", nodeID, err)
		}
		node.LastAddress = rediscAddr
		session, err = ctrl.ConnectCASE(ctx, rediscAddr, nodeID)
		if err != nil {
			ctrl.Close()
			return nil, nil, fmt.Errorf("establishing CASE session to node %d after rediscovery: %w", nodeID, err)
		}
	}

	node.LastSeen = time.Now()
	if saveErr := s.store.SaveNode(fabricID, node); saveErr != nil {
		slog.Warn("daemon: failed to update LastSeen", "node", nodeID, "err", saveErr)
	}

	client := interaction.NewClient(ctrl.Exchanges())

	s.mu.Lock()
	s.sessions[nodeID] = &cachedSession{
		ctrl:      ctrl,
		client:    client,
		session:   session,
		nodeID:    nodeID,
		addr:      node.LastAddress,
		createdAt: time.Now(),
	}
	s.mu.Unlock()

	return client, session, nil
}

// evictSession removes and closes a cached session for the given node. This is
// called when an operation fails, suggesting the session may be stale.
func (s *Server) evictSession(nodeID uint64) {
	s.mu.Lock()
	cs, ok := s.sessions[nodeID]
	if ok {
		delete(s.sessions, nodeID)
	}
	s.mu.Unlock()

	if ok && cs.ctrl != nil {
		slog.Debug("daemon: evicting session", "node", nodeID)
		cs.ctrl.Close()
	}
}

// handleInvoke processes an invoke request.
func (s *Server) handleInvoke(ctx context.Context, req *Request) Response {
	if req.Invoke == nil {
		return Response{OK: false, Error: "invoke request missing invoke field"}
	}
	if req.NodeID == 0 {
		return Response{OK: false, Error: "invoke request missing node_id"}
	}

	client, session, err := s.getOrCreateSession(ctx, req.NodeID, req.FabricID)
	if err != nil {
		s.evictSession(req.NodeID)
		return Response{OK: false, Error: fmt.Sprintf("connecting to node: %v", err)}
	}

	fields, err := DecodeFields(req.Invoke.Fields)
	if err != nil {
		return Response{OK: false, Error: fmt.Sprintf("decoding invoke fields: %v", err)}
	}

	path := interaction.NewCommandPath(req.Invoke.Endpoint, req.Invoke.ClusterID, req.Invoke.CommandID)

	invokeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var resp *interaction.InvokeResponseIB
	if req.Invoke.TimedMs > 0 {
		resp, err = client.InvokeTimed(invokeCtx, session, path, fields, req.Invoke.TimedMs)
	} else {
		resp, err = client.Invoke(invokeCtx, session, path, fields)
	}
	if err != nil {
		// The session may be stale — evict so the next attempt retries.
		s.evictSession(req.NodeID)
		return Response{OK: false, Error: fmt.Sprintf("invoke failed: %v", err)}
	}

	invokeResp := &InvokeResp{}
	if resp.Status != nil {
		invokeResp.StatusCode = resp.Status.Status.Status
	}
	if resp.Command != nil && len(resp.Command.Fields) > 0 {
		invokeResp.HasData = true
		invokeResp.Data = EncodeFields(resp.Command.Fields)
	}

	return Response{OK: true, Invoke: invokeResp}
}

// handleRead processes a read request.
func (s *Server) handleRead(ctx context.Context, req *Request) Response {
	if req.Read == nil {
		return Response{OK: false, Error: "read request missing read field"}
	}
	if req.NodeID == 0 {
		return Response{OK: false, Error: "read request missing node_id"}
	}

	client, session, err := s.getOrCreateSession(ctx, req.NodeID, req.FabricID)
	if err != nil {
		s.evictSession(req.NodeID)
		return Response{OK: false, Error: fmt.Sprintf("connecting to node: %v", err)}
	}

	paths := make([]interaction.AttributePath, len(req.Read.Paths))
	for i, p := range req.Read.Paths {
		paths[i] = interaction.NewAttributePath(p.Endpoint, p.ClusterID, p.AttributeID)
	}

	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	reports, err := client.Read(readCtx, session, paths...)
	if err != nil {
		s.evictSession(req.NodeID)
		return Response{OK: false, Error: fmt.Sprintf("read failed: %v", err)}
	}

	readResp := &ReadResp{
		Reports: make([]AttrReportResp, 0, len(reports)),
	}
	for _, r := range reports {
		rr := AttrReportResp{}
		if r.Status != nil {
			rr.Endpoint = derefU16(r.Status.Path.EndpointID)
			rr.ClusterID = derefU32(r.Status.Path.ClusterID)
			rr.AttributeID = derefU32(r.Status.Path.AttributeID)
			rr.StatusCode = r.Status.Status.Status
		}
		if r.Data != nil {
			rr.Endpoint = derefU16(r.Data.Path.EndpointID)
			rr.ClusterID = derefU32(r.Data.Path.ClusterID)
			rr.AttributeID = derefU32(r.Data.Path.AttributeID)
			rr.Data = EncodeFields(r.Data.Data)
		}
		readResp.Reports = append(readResp.Reports, rr)
	}

	return Response{OK: true, Read: readResp}
}

// handleWrite processes a write request.
func (s *Server) handleWrite(ctx context.Context, req *Request) Response {
	if req.Write == nil {
		return Response{OK: false, Error: "write request missing write field"}
	}
	if req.NodeID == 0 {
		return Response{OK: false, Error: "write request missing node_id"}
	}

	client, session, err := s.getOrCreateSession(ctx, req.NodeID, req.FabricID)
	if err != nil {
		s.evictSession(req.NodeID)
		return Response{OK: false, Error: fmt.Sprintf("connecting to node: %v", err)}
	}

	writes := make([]interaction.AttributeWrite, len(req.Write.Writes))
	for i, w := range req.Write.Writes {
		data, decErr := DecodeFields(w.Data)
		if decErr != nil {
			return Response{OK: false, Error: fmt.Sprintf("decoding write data for attribute 0x%04X: %v", w.AttributeID, decErr)}
		}
		writes[i] = interaction.AttributeWrite{
			Path: interaction.NewAttributePath(w.Endpoint, w.ClusterID, w.AttributeID),
			Data: data,
		}
	}

	writeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	statuses, err := client.Write(writeCtx, session, writes...)
	if err != nil {
		s.evictSession(req.NodeID)
		return Response{OK: false, Error: fmt.Sprintf("write failed: %v", err)}
	}

	writeResp := &WriteResp{
		Statuses: make([]AttrStatusResp, len(statuses)),
	}
	for i, st := range statuses {
		writeResp.Statuses[i] = AttrStatusResp{
			Endpoint:    derefU16(st.Path.EndpointID),
			ClusterID:   derefU32(st.Path.ClusterID),
			AttributeID: derefU32(st.Path.AttributeID),
			StatusCode:  st.Status.Status,
		}
	}

	return Response{OK: true, Write: writeResp}
}

// buildStatus returns the current daemon status.
func (s *Server) buildStatus() *StatusResp {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions := make([]SessionInfo, 0, len(s.sessions))
	for _, cs := range s.sessions {
		sessions = append(sessions, SessionInfo{
			NodeID:      cs.nodeID,
			SessionID:   cs.session.ID,
			PeerAddress: cs.addr,
			Established: Duration(time.Since(cs.createdAt)),
		})
	}

	return &StatusResp{
		Running:     true,
		Uptime:      Duration(time.Since(s.startedAt)),
		IdleTimeout: Duration(s.idleTimeout),
		Sessions:    sessions,
	}
}

// writeResponse marshals and sends a JSON response followed by a newline.
func writeResponse(conn net.Conn, resp Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		slog.Error("daemon: failed to marshal response", "error", err)
		return
	}
	data = append(data, '\n')
	_, _ = conn.Write(data)
}

// writePIDFile writes the current PID to the daemon PID file.
func writePIDFile() error {
	pidPath := PidPath()
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

// daemonRediscoverNode attempts to find a commissioned node's current address
// via mDNS operational discovery. It waits up to 5 seconds and returns the new
// "host:port" address on success.
func daemonRediscoverNode(ctx context.Context, ctrl *controller.Controller, nodeID uint64) (string, error) {
	compressedFabricID := ctrl.CompressedFabricID()
	if len(compressedFabricID) == 0 {
		return "", fmt.Errorf("controller has no fabric identity")
	}
	browser := discovery.NewMDNSBrowser()
	dev, err := browser.ResolveOperational(ctx, compressedFabricID, nodeID, 5*time.Second)
	if err != nil {
		return "", err
	}
	ip := daemonPickBestIP(dev.IPs)
	return fmt.Sprintf("%s:%d", ip.String(), dev.Port), nil
}

// daemonPickBestIP selects the preferred IP from a list: IPv6 link-local first,
// then any IPv6, then IPv4. Returns the first element if none match.
func daemonPickBestIP(ips []net.IP) net.IP {
	if len(ips) == 0 {
		return nil
	}
	var ipv6, ipv4 net.IP
	for _, ip := range ips {
		if ip.To4() == nil {
			if ip.IsLinkLocalUnicast() {
				return ip
			}
			if ipv6 == nil {
				ipv6 = ip
			}
		} else {
			if ipv4 == nil {
				ipv4 = ip
			}
		}
	}
	if ipv6 != nil {
		return ipv6
	}
	if ipv4 != nil {
		return ipv4
	}
	return ips[0]
}
