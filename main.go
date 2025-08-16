package main

import (
	"flag"
	"fmt"
	"log"
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

	// Set appropriate permissions
	if err := os.Chmod(proxySocket, 0600); err != nil && !os.IsNotExist(err) {
		log.Fatalf("Failed to set socket permissions: %v", err)
	}

	// Create the proxy
	agentProxy := proxy.NewAgentProxy(proxySocket)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start proxy in a goroutine
	go func() {
		if err := agentProxy.Start(); err != nil {
			log.Printf("Proxy error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sig := <-sigChan
	log.Printf("Received signal %v, shutting down", sig)

	// Clean up socket
	os.Remove(proxySocket)
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
