package proxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type AgentProxy struct {
	proxySocket  string
	mu           sync.RWMutex
	lastCheck    time.Time
	activeSocket string
}

func NewAgentProxy(proxySocket string) *AgentProxy {
	return &AgentProxy{
		proxySocket: proxySocket,
	}
}

func (ap *AgentProxy) FindActiveSocketCached() string {
	ap.mu.Lock()
	defer ap.mu.Unlock()

	// Only check every 5 seconds to avoid excessive filesystem scanning
	if time.Since(ap.lastCheck) < 5*time.Second && ap.activeSocket != "" {
		// Quick validation that cached socket still works
		if TestSocket(ap.activeSocket) {
			return ap.activeSocket
		}
	}

	// Find a new active socket
	activeSocket, err := FindActiveSocket()
	if err != nil {
		log.Printf("Failed to find active socket: %v", err)
		ap.activeSocket = ""
		return ""
	}

	ap.activeSocket = activeSocket
	ap.lastCheck = time.Now()
	return activeSocket
}

func (ap *AgentProxy) HandleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	activeSocket := ap.FindActiveSocketCached()
	if activeSocket == "" {
		log.Printf("No active SSH agent socket found")
		// Send SSH_AGENT_FAILURE response
		failureMsg := []byte{0, 0, 0, 1, SSH_AGENT_FAILURE}
		clientConn.Write(failureMsg)
		return
	}

	agentConn, err := net.Dial("unix", activeSocket)
	if err != nil {
		log.Printf("Failed to connect to agent socket %s: %v", activeSocket, err)
		// Send SSH_AGENT_FAILURE response
		failureMsg := []byte{0, 0, 0, 1, SSH_AGENT_FAILURE}
		clientConn.Write(failureMsg)
		return
	}
	defer agentConn.Close()

	// Bidirectional proxy
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
	<-done

	// The connection is done, let both goroutines finish naturally
}

func (ap *AgentProxy) Start() error {
	listener, err := net.Listen("unix", ap.proxySocket)
	if err != nil {
		return fmt.Errorf("failed to create proxy socket: %v", err)
	}
	defer listener.Close()

	log.Printf("SSH Agent proxy listening on %s", ap.proxySocket)

	for {
		conn, err := listener.Accept()
		if err != nil {
			// Check if error is due to closed listener
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				return nil
			}
			log.Printf("Accept error: %v", err)
			continue
		}

		go ap.HandleConnection(conn)
	}
}
