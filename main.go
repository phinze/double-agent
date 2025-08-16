package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/phinze/double-agent/proxy"
)

func main() {
	testDiscovery := flag.Bool("test-discovery", false, "Test socket discovery and exit")
	flag.Parse()

	if *testDiscovery {
		testSocketDiscovery()
		return
	}

	if len(flag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <proxy-socket-path>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  --test-discovery  Test socket discovery and exit\n")
		os.Exit(1)
	}

	proxySocket := flag.Args()[0]

	// Expand ~ to home directory
	if len(proxySocket) >= 2 && proxySocket[:2] == "~/" {
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
	
	// For Phase 2, let's show which socket we would connect to
	log.Printf("Accepted connection from %s", conn.RemoteAddr())
	
	activeSocket, err := proxy.FindActiveSocket()
	if err != nil {
		log.Printf("No active socket found: %v", err)
		errorMsg := "No active SSH agent socket found\n"
		conn.Write([]byte(errorMsg))
		return
	}
	
	log.Printf("Would forward to active socket: %s", activeSocket)
	errorMsg := fmt.Sprintf("Would forward to: %s (not yet implemented)\n", activeSocket)
	conn.Write([]byte(errorMsg))
}

func testSocketDiscovery() {
	fmt.Println("Testing SSH agent socket discovery...")
	fmt.Println()
	
	sockets, err := proxy.DiscoverSockets()
	if err != nil {
		log.Fatalf("Discovery failed: %v", err)
	}
	
	if len(sockets) == 0 {
		fmt.Println("No SSH agent sockets found")
		return
	}
	
	fmt.Printf("Found %d socket(s):\n", len(sockets))
	for _, socket := range sockets {
		status := "STALE"
		if socket.Valid {
			status = "VALID"
		}
		fmt.Printf("  %s [%s]\n", socket.Path, status)
		fmt.Printf("    Modified: %s\n", socket.ModTime.Format("2006-01-02 15:04:05"))
	}
	
	fmt.Println()
	activeSocket, err := proxy.FindActiveSocket()
	if err != nil {
		fmt.Printf("No active socket found: %v\n", err)
	} else {
		fmt.Printf("Active socket: %s\n", activeSocket)
	}
}