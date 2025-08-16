# Double Agent Implementation Plan

## Overview
Build the SSH agent proxy incrementally, with each phase producing a testable component that builds on the previous one.

## Phase 1: Basic Unix Socket Server ✅
**Goal:** Create a minimal Unix socket server that can accept connections.

### Implementation
- [x] Set up Go module structure
- [x] Create basic main.go with Unix socket listener
- [x] Handle socket cleanup on shutdown
- [x] Set proper permissions (0600)

### Verification
```bash
# Start the server
go run main.go ~/.ssh/test_agent

# In another terminal, test connection
socat - UNIX-CONNECT:~/.ssh/test_agent
# Type something and expect an error or disconnect (no handler yet)
```

---

## Phase 2: Socket Discovery & Testing ✅
**Goal:** Find and validate SSH agent sockets on the system.

### Implementation
- [x] Create `proxy/discovery.go` with socket discovery logic
- [x] Implement socket ownership validation
- [x] Sort sockets by modification time
- [x] Create `proxy/protocol.go` with SSH agent constants
- [x] Implement socket testing with `SSH_AGENTC_REQUEST_IDENTITIES`

### Verification
```bash
# Run discovery test
go run main.go --test-discovery

# Should list found sockets and their validity:
# Found socket: /tmp/ssh-XXX/agent.123 [VALID]
# Found socket: /tmp/ssh-YYY/agent.456 [STALE]
```

---

## Phase 3: Basic Proxy (Pass-through) ✅
**Goal:** Forward connections from our socket to a real SSH agent.

### Implementation
- [x] Create `proxy/proxy.go` with core proxy structure
- [x] Implement bidirectional data copying
- [x] Add connection handling goroutines
- [x] Integrate discovery to find active socket

### Verification
```bash
# Start proxy
go run main.go ~/.ssh/test_agent

# Test with ssh-add
SSH_AUTH_SOCK=~/.ssh/test_agent ssh-add -l
# Should list keys from the real agent
```

---

## Phase 4: Automatic Failover & Refresh ✅
**Goal:** Handle stale sockets and automatically find fresh ones.

### Implementation
- [x] Add periodic socket refresh (5-second cache)
- [x] Implement failover on connection errors
- [x] Add retry logic with next available socket
- [x] Handle graceful error recovery

### Verification
```bash
# Start proxy
go run main.go ~/.ssh/test_agent

# Simulate socket staleness
# 1. SSH to a machine with agent forwarding
# 2. Start proxy
# 3. Disconnect SSH
# 4. Reconnect SSH (new socket created)
# 5. Use proxy - should auto-discover new socket

SSH_AUTH_SOCK=~/.ssh/test_agent git commit -S -m "test"
```

---

## Phase 5: CLI & Configuration
**Goal:** Add proper command-line interface and configuration options.

### Implementation
- [ ] Add CLI argument parsing
- [ ] Implement verbose/debug logging
- [ ] Add daemon mode support
- [ ] Create signal handling for graceful shutdown

### Verification
```bash
# Test various CLI options
./double-agent --help
./double-agent --verbose ~/.ssh/agent
./double-agent --daemon ~/.ssh/agent
```

---

## Phase 6: Production Readiness
**Goal:** Add production features and configurations.

### Implementation
- [ ] Create systemd service file
- [ ] Add shell integration scripts
- [ ] Implement proper logging (no sensitive data)
- [ ] Add health check endpoint/mechanism
- [ ] Create Makefile for building/installing

### Verification
```bash
# Install and run as service
make install
systemctl --user start double-agent
systemctl --user status double-agent

# Test in tmux session
tmux new-session
SSH_AUTH_SOCK=~/.ssh/agent git commit -S -m "test"
```

---

## Phase 7: Testing & Documentation
**Goal:** Comprehensive testing and user documentation.

### Implementation
- [ ] Unit tests for discovery logic
- [ ] Integration tests with mock agent
- [ ] Error handling test cases
- [ ] Performance benchmarks
- [ ] Complete README with examples
- [ ] Man page or help documentation

### Verification
```bash
# Run full test suite
go test -v ./...

# Run benchmarks
go test -bench=. ./...

# Check coverage
go test -cover ./...
```

---

## Phase 8: Optional Enhancements
**Goal:** Nice-to-have features after MVP is complete.

### Ideas
- [ ] Multiple proxy socket support
- [ ] Statistics/metrics collection
- [ ] Socket activity monitoring
- [ ] Configuration file support
- [ ] Automatic tmux integration
- [ ] Socket permission inheritance
- [ ] Connection pooling

---

## Current Status

**Active Phase:** Phase 1 - Basic Unix Socket Server

**Next Steps:**
1. Create go.mod
2. Implement basic main.go
3. Test socket creation and permissions

---

## Testing Commands Reference

```bash
# Manual testing
SSH_AUTH_SOCK=~/.ssh/test_agent ssh-add -l
SSH_AUTH_SOCK=~/.ssh/test_agent ssh -T git@github.com
SSH_AUTH_SOCK=~/.ssh/test_agent git commit -S -m "test"

# Debug real agent
echo $SSH_AUTH_SOCK
ls -la $SSH_AUTH_SOCK
ssh-add -l

# Find all SSH sockets
find /tmp -type s -name "agent.*" 2>/dev/null
```

---

## Success Metrics

- [ ] Zero manual intervention in tmux sessions
- [ ] < 1ms latency overhead
- [ ] Automatic recovery from socket failures
- [ ] No security vulnerabilities
- [ ] Clear error messages
- [ ] Easy installation process