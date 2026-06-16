package proxy

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverSockets(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create test SSH agent directory structure
	sshDir := filepath.Join(tmpDir, "ssh-test1")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("Failed to create SSH dir: %v", err)
	}
	
	// Create a valid Unix socket
	socketPath := filepath.Join(sshDir, "agent.123")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to create test socket: %v", err)
	}
	defer listener.Close()
	
	// Mock agent response in a goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleMockAgentConnection(conn)
		}
	}()
	
	// Create a regular file (not a socket) that should be ignored
	regularFile := filepath.Join(sshDir, "agent.456")
	if err := os.WriteFile(regularFile, []byte("not a socket"), 0600); err != nil {
		t.Fatalf("Failed to create regular file: %v", err)
	}
	
	// DiscoverSockets globs fixed system locations (see socketGlobPatterns), so
	// we can't point it at tmpDir. Exercise socket validation directly here and
	// cover the glob patterns in TestSocketGlobPatterns.
	
	// Test socket validation
	if !TestSocket(socketPath) {
		t.Error("Expected valid socket to pass TestSocket")
	}
	
	// Test with invalid socket path
	if TestSocket("/nonexistent/socket") {
		t.Error("Expected invalid socket path to fail TestSocket")
	}
	
	// Test with regular file
	if TestSocket(regularFile) {
		t.Error("Expected regular file to fail TestSocket")
	}
}

func TestSocketGlobPatterns(t *testing.T) {
	patterns := socketGlobPatterns("/home/someone")

	// The legacy /tmp location must always be searched.
	tmpPattern := "/tmp/ssh-*/agent.*"
	// OpenSSH 10+ relocated forwarded sockets here; the regression we fixed was
	// that this location went unsearched.
	homePattern := filepath.Join("/home/someone", ".ssh", "agent", "*")

	for _, want := range []string{tmpPattern, homePattern} {
		found := false
		for _, p := range patterns {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("socketGlobPatterns missing %q; got %v", want, patterns)
		}
	}

	// With no home directory we can still fall back to the /tmp location rather
	// than emitting a bogus pattern rooted at the filesystem root.
	noHome := socketGlobPatterns("")
	for _, p := range noHome {
		if p == filepath.Join("", ".ssh", "agent", "*") {
			t.Errorf("socketGlobPatterns(\"\") produced a rootless pattern: %v", noHome)
		}
	}
}

func TestTestSocket(t *testing.T) {
	tests := []struct {
		name           string
		setupSocket    func() (string, func())
		expectedResult bool
	}{
		{
			name: "valid socket with correct response",
			setupSocket: func() (string, func()) {
				tmpDir := t.TempDir()
				socketPath := filepath.Join(tmpDir, "agent.test")
				listener, err := net.Listen("unix", socketPath)
				if err != nil {
					t.Fatalf("Failed to create socket: %v", err)
				}
				
				go func() {
					for {
						conn, err := listener.Accept()
						if err != nil {
							return
						}
						go handleMockAgentConnection(conn)
					}
				}()
				
				return socketPath, func() { listener.Close() }
			},
			expectedResult: true,
		},
		{
			name: "socket with no response",
			setupSocket: func() (string, func()) {
				tmpDir := t.TempDir()
				socketPath := filepath.Join(tmpDir, "agent.noresponse")
				listener, err := net.Listen("unix", socketPath)
				if err != nil {
					t.Fatalf("Failed to create socket: %v", err)
				}
				
				go func() {
					for {
						conn, err := listener.Accept()
						if err != nil {
							return
						}
						// Accept but don't respond
						_ = conn.Close()
					}
				}()
				
				return socketPath, func() { listener.Close() }
			},
			expectedResult: false,
		},
		{
			name: "nonexistent socket",
			setupSocket: func() (string, func()) {
				return "/tmp/nonexistent-socket", func() {}
			},
			expectedResult: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			socketPath, cleanup := tt.setupSocket()
			defer cleanup()
			
			// Small delay to ensure socket is ready
			time.Sleep(10 * time.Millisecond)
			
			result := TestSocket(socketPath)
			if result != tt.expectedResult {
				t.Errorf("TestSocket(%s) = %v, want %v", socketPath, result, tt.expectedResult)
			}
		})
	}
}

func TestFindActiveSocket(t *testing.T) {
	// This test is limited because FindActiveSocket depends on actual system sockets
	// We test it indirectly through TestSocket tests above
	
	// Create a temporary directory without any sockets
	tmpDir := t.TempDir()
	oldTmpDir := os.Getenv("TMPDIR")
	os.Setenv("TMPDIR", tmpDir)
	defer os.Setenv("TMPDIR", oldTmpDir)
	
	// Since there are no sockets in our temp dir, this should fail
	_, err := FindActiveSocket()
	if err == nil {
		t.Skip("Found actual SSH agent sockets on system, skipping negative test")
	}
	
	// Error message should indicate no sockets found
	if err.Error() != "no active SSH agent socket found" {
		t.Errorf("Unexpected error: %v", err)
	}
}

// Helper function to handle mock agent connections
func handleMockAgentConnection(conn net.Conn) {
	defer conn.Close()
	
	// Read the request
	buf := make([]byte, 5)
	n, err := conn.Read(buf)
	if err != nil || n != 5 {
		return
	}
	
	// Check if it's SSH_AGENTC_REQUEST_IDENTITIES
	if buf[4] == SSH_AGENTC_REQUEST_IDENTITIES {
		// Send SSH_AGENT_IDENTITIES_ANSWER response
		response := []byte{0, 0, 0, 5, SSH_AGENT_IDENTITIES_ANSWER, 0, 0, 0, 0}
		_, _ = conn.Write(response)
	}
}

