# Double Agent - SSH Agent Proxy Bootstrap

## Project Overview

Create a Go-based SSH agent proxy called "Double Agent" that solves the problem of stale SSH agent sockets in long-running tmux sessions. The proxy maintains a stable socket path while automatically discovering and routing to the freshest active SSH agent socket.

## Problem Statement

When working in long-running tmux sessions on remote development servers with SSH agent forwarding, the SSH_AUTH_SOCK becomes stale when reconnecting. This breaks Git commit signing and other operations that require the SSH agent. The proxy should provide a stable socket that automatically routes to the most recent active agent socket.

## Technical Requirements

### Core Functionality
- Unix domain socket proxy that forwards SSH agent protocol messages
- Automatic discovery of active SSH agent sockets in `/tmp/ssh-*/agent.*`
- Socket validation by testing with `SSH_AGENTC_REQUEST_IDENTITIES` message
- Periodic refresh of active socket (every 5 seconds max)
- Graceful handling of agent socket failures

### SSH Agent Protocol Implementation
The SSH agent wire protocol format:
```
+----------+----------+----------+
| Length   | Type     | Data     |
| (4 bytes)| (1 byte) | (varies) |
+----------+----------+----------+
```

Key message types:
- `SSH_AGENTC_REQUEST_IDENTITIES = 11`
- `SSH_AGENT_IDENTITIES_ANSWER = 12`
- `SSH_AGENTC_SIGN_REQUEST = 13`
- `SSH_AGENT_SIGN_RESPONSE = 14`

### Architecture
```
[SSH Client] → [Double Agent Proxy] → [Real SSH Agent Socket]
     ↑               ↑                        ↑
 Stable path    Auto-discovery          Forwarded socket
~/.ssh/agent   /tmp/ssh-*/agent.*      /tmp/ssh-ABC/agent.123
```

### Security Considerations
- No logging of sensitive data (keys, signatures, protocol data)
- Proper file permissions (0600 for socket, 0700 for binary)
- Socket ownership validation
- Minimal privilege operation
- Clean memory handling

## Implementation Details

### Socket Discovery Algorithm
1. Scan `/tmp/ssh-*/agent.*` for candidate sockets
2. Filter by current user ownership
3. Sort by modification time (newest first)
4. Test each socket with identity request
5. Use first working socket

### Socket Testing
Send `SSH_AGENTC_REQUEST_IDENTITIES` message:
```
[0, 0, 0, 1, 11] // length=1, type=11
```
Expect response within 1 second timeout.

### Proxy Implementation
- Bidirectional data copying between client and agent
- Connection-per-request model
- Automatic failover to next available socket on errors
- Graceful connection cleanup

## Example Implementation

```go
package main

import (
    "encoding/binary"
    "fmt"
    "io"
    "log"
    "net"
    "os"
    "os/user"
    "path/filepath"
    "sort"
    "sync"
    "syscall"
    "time"
)

type AgentProxy struct {
    proxySocket string
    mu          sync.RWMutex
    lastCheck   time.Time
    activeSocket string
}

func NewAgentProxy(proxySocket string) *AgentProxy {
    return &AgentProxy{
        proxySocket: proxySocket,
    }
}

func (ap *AgentProxy) findActiveSocket() string {
    ap.mu.Lock()
    defer ap.mu.Unlock()

    // Only check every 5 seconds to avoid excessive filesystem scanning
    if time.Since(ap.lastCheck) < 5*time.Second && ap.activeSocket != "" {
        return ap.activeSocket
    }

    sockets := ap.discoverSockets()
    if len(sockets) == 0 {
        ap.activeSocket = ""
        return ""
    }

    // Test sockets to find a working one
    for _, socket := range sockets {
        if ap.testSocket(socket) {
            ap.activeSocket = socket
            ap.lastCheck = time.Now()
            return socket
        }
    }

    ap.activeSocket = ""
    return ""
}

func (ap *AgentProxy) discoverSockets() []string {
    var sockets []string

    currentUser, err := user.Current()
    if err != nil {
        return sockets
    }

    // Look for SSH agent sockets in /tmp
    pattern := "/tmp/ssh-*/agent.*"
    matches, err := filepath.Glob(pattern)
    if err != nil {
        return sockets
    }

    // Filter by ownership and sort by modification time (newest first)
    type socketInfo struct {
        path    string
        modTime time.Time
    }

    var validSockets []socketInfo
    for _, match := range matches {
        info, err := os.Stat(match)
        if err != nil {
            continue
        }

        // Check if socket is owned by current user
        if stat, ok := info.Sys().(*syscall.Stat_t); ok {
            if fmt.Sprintf("%d", stat.Uid) == currentUser.Uid {
                validSockets = append(validSockets, socketInfo{
                    path:    match,
                    modTime: info.ModTime(),
                })
            }
        }
    }

    // Sort by modification time (newest first)
    sort.Slice(validSockets, func(i, j int) bool {
        return validSockets[i].modTime.After(validSockets[j].modTime)
    })

    for _, socket := range validSockets {
        sockets = append(sockets, socket.path)
    }

    return sockets
}

func (ap *AgentProxy) testSocket(socketPath string) bool {
    conn, err := net.Dial("unix", socketPath)
    if err != nil {
        return false
    }
    defer conn.Close()

    // Send SSH_AGENTC_REQUEST_IDENTITIES message
    msg := []byte{0, 0, 0, 1, 11} // length=1, type=SSH_AGENTC_REQUEST_IDENTITIES

    _, err = conn.Write(msg)
    if err != nil {
        return false
    }

    // Try to read response header
    header := make([]byte, 5)
    conn.SetReadDeadline(time.Now().Add(1 * time.Second))
    _, err = io.ReadFull(conn, header)

    return err == nil
}

func (ap *AgentProxy) handleConnection(clientConn net.Conn) {
    defer clientConn.Close()

    activeSocket := ap.findActiveSocket()
    if activeSocket == "" {
        log.Printf("No active SSH agent socket found")
        return
    }

    agentConn, err := net.Dial("unix", activeSocket)
    if err != nil {
        log.Printf("Failed to connect to agent socket %s: %v", activeSocket, err)
        return
    }
    defer agentConn.Close()

    // Bidirectional proxy
    done := make(chan struct{}, 2)

    go func() {
        io.Copy(agentConn, clientConn)
        done <- struct{}{}
    }()

    go func() {
        io.Copy(clientConn, agentConn)
        done <- struct{}{}
    }()

    <-done
}

func (ap *AgentProxy) Start() error {
    // Remove existing socket
    os.Remove(ap.proxySocket)

    // Create directory if it doesn't exist
    os.MkdirAll(filepath.Dir(ap.proxySocket), 0700)

    listener, err := net.Listen("unix", ap.proxySocket)
    if err != nil {
        return fmt.Errorf("failed to create proxy socket: %v", err)
    }
    defer listener.Close()

    // Set appropriate permissions
    os.Chmod(ap.proxySocket, 0600)

    log.Printf("SSH Agent proxy listening on %s", ap.proxySocket)

    for {
        conn, err := listener.Accept()
        if err != nil {
            log.Printf("Accept error: %v", err)
            continue
        }

        go ap.handleConnection(conn)
    }
}

func main() {
    if len(os.Args) != 2 {
        fmt.Fprintf(os.Stderr, "Usage: %s <proxy-socket-path>\n", os.Args[0])
        os.Exit(1)
    }

    proxySocket := os.Args[1]
    proxy := NewAgentProxy(proxySocket)

    if err := proxy.Start(); err != nil {
        log.Fatalf("Proxy failed: %v", err)
    }
}
```

## Project Structure
```
double-agent/
├── main.go              # Entry point and CLI
├── proxy/
│   ├── proxy.go         # Core proxy logic
│   ├── discovery.go     # Socket discovery
│   └── protocol.go      # SSH agent protocol constants
├── go.mod
├── go.sum
├── README.md
├── LICENSE
└── examples/
    ├── systemd/
    │   └── double-agent.service
    └── shell/
        └── setup.sh
```

## Command Line Interface
```bash
double-agent [options] <proxy-socket-path>

Options:
  -v, --verbose     Enable verbose logging
  -d, --daemon      Run as daemon
  -h, --help        Show help

Example:
  double-agent ~/.ssh/agent_sock
```

## Usage Integration
```bash
# Set in shell profile
export SSH_AUTH_SOCK="$HOME/.ssh/agent_sock"

# Start proxy (manually or via systemd)
double-agent ~/.ssh/agent_sock

# All SSH operations now use stable socket
git commit -S -m "Signed commit"
ssh production-server
```

## Testing Strategy
- Unit tests for socket discovery logic
- Integration tests with mock SSH agent
- Error handling tests (missing sockets, permission errors)
- Performance tests for socket switching latency

## Success Criteria
1. Transparent SSH agent operation through proxy
2. Automatic failover when SSH connections change
3. No manual intervention required in tmux sessions
4. Minimal performance overhead (< 1ms latency)
5. Robust error handling and recovery

## Deliverables
- Complete Go implementation
- Comprehensive README with setup instructions
- Example systemd service file
- Shell integration examples
- Basic test suite

## Getting Started with Claude Code
This prompt provides the foundation for implementing Double Agent. Focus on creating a robust, security-conscious implementation that prioritizes reliability and transparency of operation.

### Recommended Claude Code Commands
```bash
# Initialize project
claude-code create go-project double-agent

# Generate initial structure
claude-code generate "Create the project structure with main.go, proxy package, and example configurations based on the bootstrap documentation"

# Implement core features
claude-code implement "Build the SSH agent proxy with socket discovery and protocol handling"

# Add testing
claude-code test "Create comprehensive tests for socket discovery, proxy functionality, and error handling"
```
