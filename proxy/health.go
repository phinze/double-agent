package proxy

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"time"
)

// HealthCheck performs a health check on the proxy socket
func HealthCheck(socketPath string, logger *slog.Logger) error {
	// Try to connect to the socket
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to proxy socket: %v", err)
	}
	defer conn.Close()

	// Send SSH_AGENTC_REQUEST_IDENTITIES request
	request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
	if _, err := conn.Write(request); err != nil {
		return fmt.Errorf("failed to send health check request: %v", err)
	}

	// Set read timeout
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Read response header (4 bytes for length)
	header := make([]byte, 4)
	if _, err := conn.Read(header); err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	// Parse response length
	respLen := binary.BigEndian.Uint32(header)
	if respLen > 1024*1024 { // Sanity check: response shouldn't be larger than 1MB
		return fmt.Errorf("invalid response length: %d", respLen)
	}

	// Read response body
	response := make([]byte, respLen)
	if _, err := conn.Read(response); err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}

	// Check if we got a valid response type
	if len(response) > 0 {
		responseType := response[0]
		switch responseType {
		case SSH_AGENT_IDENTITIES_ANSWER:
			// Success - the proxy forwarded our request and got a valid response
			return nil
		case SSH_AGENT_FAILURE:
			// The proxy is working but no agent is available
			return fmt.Errorf("proxy is running but no active SSH agent found")
		default:
			return fmt.Errorf("unexpected response type: %d", responseType)
		}
	}

	return fmt.Errorf("empty response from proxy")
}

// IsHealthy checks if the proxy is healthy (convenience wrapper)
func IsHealthy(socketPath string, logger *slog.Logger) bool {
	err := HealthCheck(socketPath, logger)
	if err != nil {
		logger.Debug("Health check failed", "error", err)
		return false
	}
	return true
}
