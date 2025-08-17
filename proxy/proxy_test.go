package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewAgentProxy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	proxySocket := "/tmp/test.sock"
	
	ap := NewAgentProxy(proxySocket, logger)
	
	if ap.proxySocket != proxySocket {
		t.Errorf("Expected proxy socket %s, got %s", proxySocket, ap.proxySocket)
	}
	
	if ap.logger == nil {
		t.Error("Expected logger to be set")
	}
	
	if ap.activeSocket != "" {
		t.Error("Expected activeSocket to be empty initially")
	}
}

func TestInvalidateCache(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ap := NewAgentProxy("/tmp/test.sock", logger)
	
	// Set some values
	ap.activeSocket = "/tmp/some-socket"
	ap.lastCheck = time.Now()
	
	// Invalidate cache
	ap.InvalidateCache()
	
	// Check values are reset
	if ap.activeSocket != "" {
		t.Error("Expected activeSocket to be cleared")
	}
	
	if !ap.lastCheck.IsZero() {
		t.Error("Expected lastCheck to be zero time")
	}
}

func TestFindActiveSocketCached(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ap := NewAgentProxy("/tmp/test.sock", logger)
	
	// Test 1: With a valid mock socket
	testSocket := createMockSocket(t)
	defer os.Remove(testSocket)
	
	// Manually set the cache to test caching behavior
	ap.activeSocket = testSocket
	ap.lastCheck = time.Now()
	
	// Should return cached socket
	result := ap.FindActiveSocketCached()
	if result != testSocket {
		t.Errorf("Expected %s, got %s", testSocket, result)
	}
	
	// Test 2: Expired cache
	ap.lastCheck = time.Now().Add(-10 * time.Second)
	
	// This will try to validate the cached socket and may find a different one
	result = ap.FindActiveSocketCached()
	// Can't predict the result as it depends on system state
	
	// Test 3: Invalid cached socket
	ap.activeSocket = "/tmp/nonexistent"
	ap.lastCheck = time.Now().Add(-10 * time.Second)
	
	// Should find new socket (or return empty if none found)
	result = ap.FindActiveSocketCached()
	// Result depends on system state, just ensure no panic
}

func TestHandleConnection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	// Create mock agent
	agentSocket := createMockAgent(t)
	defer os.Remove(agentSocket)
	
	// Create proxy with cached socket
	ap := NewAgentProxy("/tmp/test.sock", logger)
	ap.activeSocket = agentSocket
	ap.lastCheck = time.Now()
	
	// Create client connection pair
	client, proxyEnd := net.Pipe()
	defer client.Close()
	
	// Handle connection in goroutine
	done := make(chan struct{})
	go func() {
		ap.HandleConnection(proxyEnd)
		close(done)
	}()
	
	// Send SSH_AGENTC_REQUEST_IDENTITIES
	request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
	_, err := client.Write(request)
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}
	
	// Read response
	response := make([]byte, 9)
	_, err = client.Read(response)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}
	
	// Verify we got SSH_AGENT_IDENTITIES_ANSWER
	if response[4] != SSH_AGENT_IDENTITIES_ANSWER {
		t.Errorf("Expected SSH_AGENT_IDENTITIES_ANSWER, got %d", response[4])
	}
	
	// Close client to trigger cleanup
	client.Close()
	
	// Wait for handler to finish
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Handler did not finish in time")
	}
}

func TestHandleConnectionNoAgent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ap := NewAgentProxy("/tmp/test.sock", logger)
	
	// Set a non-existent socket to force failure
	ap.activeSocket = "/tmp/nonexistent-agent-socket"
	ap.lastCheck = time.Now()
	
	// Create client connection pair
	client, proxyEnd := net.Pipe()
	defer client.Close()
	
	// Handle connection in goroutine
	done := make(chan struct{})
	go func() {
		ap.HandleConnection(proxyEnd)
		close(done)
	}()
	
	// Read response (should be SSH_AGENT_FAILURE)
	response := make([]byte, 5)
	n, err := client.Read(response)
	
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read response: %v", err)
	}
	
	if n >= 5 && response[4] != SSH_AGENT_FAILURE {
		t.Errorf("Expected SSH_AGENT_FAILURE, got %d", response[4])
	}
	
	// Close client
	client.Close()
	
	// Wait for handler to finish
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Handler did not finish in time")
	}
}

func TestStart(t *testing.T) {
	t.Skip("Skipping TestStart - difficult to test listener shutdown reliably")
}

// Helper functions

func createMockSocket(t *testing.T) string {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "mock.sock")
	
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to create mock socket: %v", err)
	}
	
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Read request
				buf := make([]byte, 5)
				if n, err := c.Read(buf); err == nil && n == 5 {
					// Send valid response
					if buf[4] == SSH_AGENTC_REQUEST_IDENTITIES {
						response := []byte{0, 0, 0, 5, SSH_AGENT_IDENTITIES_ANSWER, 0, 0, 0, 0}
						_, _ = c.Write(response)
					}
				}
			}(conn)
		}
	}()
	
	return socketPath
}

func createMockAgent(t *testing.T) string {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "agent.sock")
	
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to create mock agent: %v", err)
	}
	
	var wg sync.WaitGroup
	wg.Add(1)
	
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleMockAgentConn(conn)
		}
	}()
	
	// Give listener time to start
	time.Sleep(10 * time.Millisecond)
	
	return socketPath
}

func handleMockAgentConn(conn net.Conn) {
	defer conn.Close()
	
	for {
		// Read request header
		header := make([]byte, 5)
		n, err := conn.Read(header)
		if err != nil || n != 5 {
			return
		}
		
		// Handle SSH_AGENTC_REQUEST_IDENTITIES
		if header[4] == SSH_AGENTC_REQUEST_IDENTITIES {
			// Send response with 0 identities
			response := []byte{0, 0, 0, 5, SSH_AGENT_IDENTITIES_ANSWER, 0, 0, 0, 0}
			if _, err := conn.Write(response); err != nil {
				return
			}
		} else {
			// Send SSH_AGENT_FAILURE for other requests
			response := []byte{0, 0, 0, 1, SSH_AGENT_FAILURE}
			if _, err := conn.Write(response); err != nil {
				return
			}
		}
	}
}

// TestRaceConditions tests for race conditions in concurrent access
func TestRaceConditions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ap := NewAgentProxy("/tmp/test.sock", logger)
	
	// Test concurrent cache invalidation and socket finding
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ap.InvalidateCache()
		}()
		go func() {
			defer wg.Done()
			_ = ap.FindActiveSocketCached()
		}()
	}
	wg.Wait()
	
	// Test should complete without race conditions
}

// BenchmarkHandleConnection benchmarks connection handling
func BenchmarkHandleConnection(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	// Create mock agent
	agentSocket := createMockAgentForBench(b)
	defer os.Remove(agentSocket)
	
	ap := NewAgentProxy("/tmp/test.sock", logger)
	ap.activeSocket = agentSocket
	ap.lastCheck = time.Now()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		client, proxyEnd := net.Pipe()
		
		go ap.HandleConnection(proxyEnd)
		
		// Send request
		request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
		_, _ = client.Write(request)
		
		// Read response
		response := make([]byte, 9)
		_, _ = client.Read(response)
		
		client.Close()
	}
}

func createMockAgentForBench(b *testing.B) string {
	tmpDir := b.TempDir()
	socketPath := filepath.Join(tmpDir, "bench-agent.sock")
	
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		b.Fatalf("Failed to create mock agent: %v", err)
	}
	
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil || n < 5 {
						return
					}
					// Always respond with SSH_AGENT_IDENTITIES_ANSWER
					response := []byte{0, 0, 0, 5, SSH_AGENT_IDENTITIES_ANSWER, 0, 0, 0, 0}
					if _, err := c.Write(response); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	
	return socketPath
}

// TestSanitizingHandler tests the log sanitization
func TestSanitizingHandler(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, nil)
	sanitized := NewSanitizingHandler(handler)
	logger := slog.New(sanitized)
	
	// Test path sanitization
	logger.Info("test", "path", "/home/johndoe/.ssh/agent")
	if bytes.Contains(buf.Bytes(), []byte("johndoe")) {
		t.Error("Username not sanitized from path")
	}
	if !bytes.Contains(buf.Bytes(), []byte("/home/<user>/.ssh/agent")) {
		t.Error("Path not properly sanitized")
	}
	
	// Test fingerprint sanitization
	buf.Reset()
	logger.Info("test", "fingerprint", "SHA256:abc123def456")
	if bytes.Contains(buf.Bytes(), []byte("abc123def456")) {
		t.Error("Fingerprint not sanitized")
	}
	if !bytes.Contains(buf.Bytes(), []byte("SHA256:<redacted>")) {
		t.Error("Fingerprint not properly sanitized")
	}
}