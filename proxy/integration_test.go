// +build integration

package proxy

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestFullProxyIntegration tests the complete flow from client through proxy to agent
func TestFullProxyIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	// Step 1: Create a full mock SSH agent
	agentAddr := startFullMockAgent(t)
	
	// Step 2: Start the proxy with our mock agent
	tmpDir := t.TempDir()
	proxySocket := filepath.Join(tmpDir, "proxy.sock")
	
	ap := NewAgentProxy(proxySocket, logger)
	ap.activeSocket = agentAddr
	ap.lastCheck = time.Now()
	
	// Start proxy in background
	proxyErr := make(chan error, 1)
	go func() {
		proxyErr <- ap.Start()
	}()
	
	// Wait for proxy to be ready
	time.Sleep(100 * time.Millisecond)
	
	// Step 4: Connect as a client and perform operations
	t.Run("RequestIdentities", func(t *testing.T) {
		conn, err := net.Dial("unix", proxySocket)
		if err != nil {
			t.Fatalf("Failed to connect to proxy: %v", err)
		}
		defer conn.Close()
		
		// Send SSH_AGENTC_REQUEST_IDENTITIES
		request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
		if _, err := conn.Write(request); err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		
		// Read response
		header := make([]byte, 4)
		if _, err := io.ReadFull(conn, header); err != nil {
			t.Fatalf("Failed to read response header: %v", err)
		}
		
		length := binary.BigEndian.Uint32(header)
		if length > 1024 {
			t.Fatalf("Response too large: %d", length)
		}
		
		body := make([]byte, length)
		if _, err := io.ReadFull(conn, body); err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}
		
		if body[0] != SSH_AGENT_IDENTITIES_ANSWER {
			t.Errorf("Expected SSH_AGENT_IDENTITIES_ANSWER, got %d", body[0])
		}
	})
	
	t.Run("ConcurrentConnections", func(t *testing.T) {
		var wg sync.WaitGroup
		errors := make(chan error, 10)
		
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				
				conn, err := net.Dial("unix", proxySocket)
				if err != nil {
					errors <- fmt.Errorf("client %d: failed to connect: %w", id, err)
					return
				}
				defer conn.Close()
				
				// Send request
				request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
				if _, err := conn.Write(request); err != nil {
					errors <- fmt.Errorf("client %d: failed to send: %w", id, err)
					return
				}
				
				// Read response
				response := make([]byte, 9)
				if _, err := io.ReadFull(conn, response); err != nil {
					errors <- fmt.Errorf("client %d: failed to read: %w", id, err)
					return
				}
				
				if response[4] != SSH_AGENT_IDENTITIES_ANSWER {
					errors <- fmt.Errorf("client %d: wrong response type: %d", id, response[4])
				}
			}(i)
		}
		
		wg.Wait()
		close(errors)
		
		for err := range errors {
			t.Error(err)
		}
	})
	
	t.Run("AgentFailover", func(t *testing.T) {
		// Simulate agent going away and coming back
		oldAgent := agentAddr
		
		// Point to non-existent socket
		ap.activeSocket = "/tmp/nonexistent-agent"
		ap.lastCheck = time.Now().Add(-10 * time.Second) // Force re-validation
		
		conn, err := net.Dial("unix", proxySocket)
		if err != nil {
			t.Fatalf("Failed to connect to proxy: %v", err)
		}
		defer conn.Close()
		
		// This should trigger failover attempt
		request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
		if _, err := conn.Write(request); err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		
		// Should get SSH_AGENT_FAILURE
		response := make([]byte, 5)
		if _, err := io.ReadFull(conn, response); err != nil && err != io.EOF {
			t.Fatalf("Failed to read response: %v", err)
		}
		
		if len(response) >= 5 && response[4] != SSH_AGENT_FAILURE {
			t.Errorf("Expected SSH_AGENT_FAILURE during failover, got %d", response[4])
		}
		
		// Restore agent
		ap.activeSocket = oldAgent
		ap.lastCheck = time.Now()
	})
	
	// Cleanup
	os.Remove(proxySocket)
}

// TestProxyHealthCheck tests the health check functionality
func TestProxyHealthCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	// Start mock agent
	agentAddr := startFullMockAgent(t)
	
	// Start proxy with cached agent
	tmpDir := t.TempDir()
	proxySocket := filepath.Join(tmpDir, "proxy.sock")
	
	ap := NewAgentProxy(proxySocket, logger)
	ap.activeSocket = agentAddr
	ap.lastCheck = time.Now()
	
	go ap.Start()
	time.Sleep(100 * time.Millisecond)
	
	// Perform health check
	if err := HealthCheck(proxySocket, logger); err != nil {
		t.Errorf("Health check failed: %v", err)
	}
	
	// Test IsHealthy wrapper
	if !IsHealthy(proxySocket, logger) {
		t.Error("IsHealthy returned false for healthy proxy")
	}
	
	// Test health check on non-existent socket
	if err := HealthCheck("/tmp/nonexistent", logger); err == nil {
		t.Error("Expected health check to fail for non-existent socket")
	}
	
	os.Remove(proxySocket)
}

// TestProxyPerformance tests proxy performance under load
func TestProxyPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	// Start mock agent
	agentAddr := startFullMockAgent(t)
	
	// Start proxy with cached agent
	tmpDir := t.TempDir()
	proxySocket := filepath.Join(tmpDir, "proxy.sock")
	
	ap := NewAgentProxy(proxySocket, logger)
	ap.activeSocket = agentAddr
	ap.lastCheck = time.Now()
	go ap.Start()
	time.Sleep(100 * time.Millisecond)
	
	// Measure latency
	iterations := 100
	var totalDuration time.Duration
	
	for i := 0; i < iterations; i++ {
		start := time.Now()
		
		conn, err := net.Dial("unix", proxySocket)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		
		request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
		if _, err := conn.Write(request); err != nil {
			conn.Close()
			t.Fatalf("Failed to send request: %v", err)
		}
		
		response := make([]byte, 9)
		if _, err := io.ReadFull(conn, response); err != nil {
			conn.Close()
			t.Fatalf("Failed to read response: %v", err)
		}
		
		conn.Close()
		
		duration := time.Since(start)
		totalDuration += duration
	}
	
	avgLatency := totalDuration / time.Duration(iterations)
	t.Logf("Average latency: %v", avgLatency)
	
	// Check if latency is under 1ms (goal from PLAN.md)
	if avgLatency > 1*time.Millisecond {
		t.Logf("Warning: Average latency %v exceeds 1ms target", avgLatency)
	}
	
	os.Remove(proxySocket)
}

// startFullMockAgent starts a complete mock SSH agent for integration testing
func startFullMockAgent(t *testing.T) string {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "mock-agent.sock")
	
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to create mock agent socket: %v", err)
	}
	
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleFullMockAgentConnection(conn)
		}
	}()
	
	// Ensure agent is ready
	time.Sleep(50 * time.Millisecond)
	
	// Test that agent is working
	if !TestSocket(socketPath) {
		t.Fatal("Mock agent not responding correctly")
	}
	
	return socketPath
}

func handleFullMockAgentConnection(conn net.Conn) {
	defer conn.Close()
	
	for {
		// Read request header
		header := make([]byte, 4)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		
		length := binary.BigEndian.Uint32(header)
		if length > 1024*1024 {
			return // Invalid request
		}
		
		// Read request body
		body := make([]byte, length)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		
		if len(body) == 0 {
			return
		}
		
		requestType := body[0]
		
		var response []byte
		switch requestType {
		case SSH_AGENTC_REQUEST_IDENTITIES:
			// Return empty identities list
			response = buildIdentitiesResponse()
		case SSH_AGENTC_ADD_IDENTITY:
			// Pretend to add identity
			response = []byte{0, 0, 0, 1, SSH_AGENT_SUCCESS}
		case SSH_AGENTC_REMOVE_IDENTITY:
			// Pretend to remove identity
			response = []byte{0, 0, 0, 1, SSH_AGENT_SUCCESS}
		case SSH_AGENTC_REMOVE_ALL_IDENTITIES:
			// Pretend to remove all identities
			response = []byte{0, 0, 0, 1, SSH_AGENT_SUCCESS}
		default:
			// Unknown request
			response = []byte{0, 0, 0, 1, SSH_AGENT_FAILURE}
		}
		
		if _, err := conn.Write(response); err != nil {
			return
		}
	}
}

func buildIdentitiesResponse() []byte {
	var buf bytes.Buffer
	
	// Write response type
	buf.WriteByte(SSH_AGENT_IDENTITIES_ANSWER)
	
	// Write number of identities (0 for empty)
	binary.Write(&buf, binary.BigEndian, uint32(0))
	
	// Prepend length
	data := buf.Bytes()
	response := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(response, uint32(len(data)))
	copy(response[4:], data)
	
	return response
}

// TestEdgeCases tests various edge cases
func TestEdgeCases(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping edge case tests in short mode")
	}
	
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	t.Run("LargeRequest", func(t *testing.T) {
		// Start mock agent that handles large requests
		agentAddr := startFullMockAgent(t)
		
		tmpDir := t.TempDir()
		proxySocket := filepath.Join(tmpDir, "proxy.sock")
		
		ap := NewAgentProxy(proxySocket, logger)
		ap.activeSocket = agentAddr
		ap.lastCheck = time.Now()
		go ap.Start()
		time.Sleep(100 * time.Millisecond)
		
		conn, err := net.Dial("unix", proxySocket)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()
		
		// Send a large (but valid) request
		largeData := make([]byte, 1024)
		for i := range largeData {
			largeData[i] = byte(i % 256)
		}
		
		request := make([]byte, 4+len(largeData))
		binary.BigEndian.PutUint32(request, uint32(len(largeData)))
		copy(request[4:], largeData)
		
		if _, err := conn.Write(request); err != nil {
			t.Fatalf("Failed to send large request: %v", err)
		}
		
		// Should get a response (likely SSH_AGENT_FAILURE)
		response := make([]byte, 5)
		if _, err := io.ReadFull(conn, response); err != nil && err != io.EOF {
			t.Fatalf("Failed to read response: %v", err)
		}
		
		os.Remove(proxySocket)
	})
	
	t.Run("RapidReconnection", func(t *testing.T) {
		agentAddr := startFullMockAgent(t)
		
		tmpDir := t.TempDir()
		proxySocket := filepath.Join(tmpDir, "proxy.sock")
		
		ap := NewAgentProxy(proxySocket, logger)
		ap.activeSocket = agentAddr
		ap.lastCheck = time.Now()
		go ap.Start()
		time.Sleep(100 * time.Millisecond)
		
		// Rapidly connect and disconnect
		for i := 0; i < 20; i++ {
			conn, err := net.Dial("unix", proxySocket)
			if err != nil {
				t.Fatalf("Failed to connect on iteration %d: %v", i, err)
			}
			conn.Close()
		}
		
		os.Remove(proxySocket)
	})
}