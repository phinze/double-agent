package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <proxy-socket-path>\n", os.Args[0])
		os.Exit(1)
	}

	proxySocket := os.Args[1]

	// Expand ~ to home directory
	if proxySocket[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get home directory: %v", err)
		}
		proxySocket = filepath.Join(home, proxySocket[2:])
	}

	// Remove existing socket if it exists
	if err := os.Remove(proxySocket); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: failed to remove existing socket: %v", err)
	}

	// Create directory if it doesn't exist
	socketDir := filepath.Dir(proxySocket)
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		log.Fatalf("Failed to create socket directory: %v", err)
	}

	// Create Unix domain socket listener
	listener, err := net.Listen("unix", proxySocket)
	if err != nil {
		log.Fatalf("Failed to create proxy socket: %v", err)
	}
	defer listener.Close()

	// Set appropriate permissions (owner read/write only)
	if err := os.Chmod(proxySocket, 0600); err != nil {
		log.Fatalf("Failed to set socket permissions: %v", err)
	}

	log.Printf("Double Agent proxy listening on %s", proxySocket)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Accept connections in a goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				// Check if error is due to closed listener
				if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
					return
				}
				log.Printf("Accept error: %v", err)
				continue
			}

			// Handle connection
			go handleConnection(conn)
		}
	}()

	// Wait for shutdown signal
	sig := <-sigChan
	log.Printf("Received signal %v, shutting down", sig)

	// Clean up socket
	listener.Close()
	os.Remove(proxySocket)
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	
	// For Phase 1, just acknowledge the connection and close
	log.Printf("Accepted connection from %s", conn.RemoteAddr())
	
	// Send a simple error message for now
	// In later phases, this will forward to the real SSH agent
	errorMsg := "SSH agent proxy not yet implemented\n"
	conn.Write([]byte(errorMsg))
}