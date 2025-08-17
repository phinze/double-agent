package proxy

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

type SocketInfo struct {
	Path    string
	ModTime time.Time
	Valid   bool
}

func DiscoverSockets() ([]SocketInfo, error) {
	var sockets []SocketInfo

	currentUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	// Look for SSH agent sockets in /tmp
	pattern := "/tmp/ssh-*/agent.*"
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob for sockets: %w", err)
	}

	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}

		// Check if it's actually a socket
		if info.Mode()&os.ModeSocket == 0 {
			continue
		}

		// Check if socket is owned by current user
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			if fmt.Sprintf("%d", stat.Uid) != currentUser.Uid {
				continue
			}

			socketInfo := SocketInfo{
				Path:    match,
				ModTime: info.ModTime(),
				Valid:   false, // Will be validated later
			}
			sockets = append(sockets, socketInfo)
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(sockets, func(i, j int) bool {
		return sockets[i].ModTime.After(sockets[j].ModTime)
	})

	// Validate each socket
	for i := range sockets {
		sockets[i].Valid = TestSocket(sockets[i].Path)
	}

	return sockets, nil
}

func TestSocket(socketPath string) bool {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()

	// Send SSH_AGENTC_REQUEST_IDENTITIES message
	// Format: [length (4 bytes)][type (1 byte)]
	msg := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}

	_, err = conn.Write(msg)
	if err != nil {
		return false
	}

	// Try to read response header (5 bytes: 4 for length, 1 for type)
	header := make([]byte, 5)
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := io.ReadFull(conn, header)

	// Check if we got a valid response
	if err != nil || n != 5 {
		return false
	}

	// Check if response type is SSH_AGENT_IDENTITIES_ANSWER or SSH_AGENT_FAILURE
	responseType := header[4]
	return responseType == SSH_AGENT_IDENTITIES_ANSWER || responseType == SSH_AGENT_FAILURE
}

func FindActiveSocket() (string, error) {
	sockets, err := DiscoverSockets()
	if err != nil {
		return "", err
	}

	for _, socket := range sockets {
		if socket.Valid {
			return socket.Path, nil
		}
	}

	return "", fmt.Errorf("no active SSH agent socket found")
}
