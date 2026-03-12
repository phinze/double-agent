package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

type AgentProxy struct {
	proxySocket  string
	mu           sync.RWMutex
	lastCheck    time.Time
	activeSocket string
	logger       *slog.Logger
}

func NewAgentProxy(proxySocket string, logger *slog.Logger) *AgentProxy {
	return &AgentProxy{
		proxySocket: proxySocket,
		logger:      logger,
	}
}

func (ap *AgentProxy) InvalidateCache() {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	ap.activeSocket = ""
	ap.lastCheck = time.Time{}
}

func (ap *AgentProxy) FindActiveSocketCached() string {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	// Return cached socket if still within TTL. HandleConnection's retry
	// logic will invalidate the cache if the socket turns out to be stale.
	// We intentionally avoid re-validating with TestSocket here because
	// some SSH agent forwarding implementations (e.g., Blink) cannot
	// accept a new connection immediately after one closes.
	if time.Since(ap.lastCheck) < 5*time.Second && ap.activeSocket != "" {
		return ap.activeSocket
	}

	// Find a new active socket (TestSocket is called during discovery)
	activeSocket, err := FindActiveSocket()
	if err != nil {
		ap.logger.Error("Failed to find active socket", "error", err)
		ap.activeSocket = ""
		return ""
	}

	if ap.activeSocket != activeSocket {
		ap.logger.Info("Active socket changed",
			"from", ap.activeSocket,
			"to", activeSocket)
	}

	ap.activeSocket = activeSocket
	ap.lastCheck = time.Now()

	// Brief pause after discovery to allow agent forwarding implementations
	// to recover from the TestSocket validation connection.
	time.Sleep(15 * time.Millisecond)

	return activeSocket
}

func (ap *AgentProxy) HandleConnection(clientConn net.Conn) {
	defer func() { _ = clientConn.Close() }()

	// Try up to 2 times (once with cached, once with fresh discovery)
	for attempt := 0; attempt < 2; attempt++ {
		activeSocket := ap.FindActiveSocketCached()
		if activeSocket == "" {
			if attempt == 0 {
				ap.logger.Debug("No active SSH agent socket found, retrying discovery",
					"attempt", attempt+1)
			} else {
				// Final attempt failed - log prominently
				ap.logger.Warn("No active SSH agent socket available",
					"hint", "Run 'double-agent --test-discovery' to diagnose. Common causes: stale forwarded socket, agent timeout on slow connection, or no SSH agent forwarding.")
				// Send SSH_AGENT_FAILURE response after final attempt
				failureMsg := []byte{0, 0, 0, 1, SSH_AGENT_FAILURE}
				if _, err := clientConn.Write(failureMsg); err != nil {
					ap.logger.Debug("Failed to send agent failure response to client",
						"error", err)
				}
			}
			continue
		}

		agentConn, err := net.Dial("unix", activeSocket)
		if err != nil {
			ap.logger.Debug("Failed to connect to agent socket",
				"socket", activeSocket,
				"error", err,
				"attempt", attempt+1)
			// Invalidate cache so next attempt finds a fresh socket
			ap.InvalidateCache()
			if attempt == 1 {
				// Send SSH_AGENT_FAILURE response after final attempt
				failureMsg := []byte{0, 0, 0, 1, SSH_AGENT_FAILURE}
				if _, err := clientConn.Write(failureMsg); err != nil {
					ap.logger.Debug("Failed to send agent failure response to client",
						"error", err)
				}
			}
			continue
		}
		defer func() { _ = agentConn.Close() }()

		// Successfully connected, proceed with proxy
		done := make(chan error, 2)

		// Copy from client to agent
		go func() {
			_, err := io.Copy(agentConn, clientConn)
			done <- err
		}()

		// Copy from agent to client
		go func() {
			_, err := io.Copy(clientConn, agentConn)
			done <- err
		}()

		// Wait for one side to finish
		err = <-done

		// If we had an error during communication, invalidate cache
		if err != nil && err != io.EOF {
			ap.logger.Debug("Connection error", "error", err)
			ap.InvalidateCache()
		}

		// Connection handled successfully
		return
	}
}

func (ap *AgentProxy) Start() error {
	listener, err := net.Listen("unix", ap.proxySocket)
	if err != nil {
		return fmt.Errorf("failed to create proxy socket: %v", err)
	}
	defer func() { _ = listener.Close() }()

	ap.logger.Info("SSH Agent proxy listening", "socket", ap.proxySocket)

	for {
		conn, err := listener.Accept()
		if err != nil {
			// Check if error is due to closed listener
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				return nil
			}
			ap.logger.Error("Accept error", "error", err)
			continue
		}

		go ap.HandleConnection(conn)
	}
}
