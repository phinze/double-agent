package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/phinze/double-agent/proxy"
)

var (
	version = "dev" // Can be overridden at build time
)

func main() {
	var (
		verbose       = flag.Bool("v", false, "Enable verbose logging")
		verboseLong   = flag.Bool("verbose", false, "Enable verbose logging")
		daemon        = flag.Bool("d", false, "Run as daemon (detach from terminal)")
		daemonLong    = flag.Bool("daemon", false, "Run as daemon (detach from terminal)")
		testDiscovery = flag.Bool("test-discovery", false, "Test socket discovery and exit")
		healthCheck   = flag.Bool("health", false, "Check if proxy is healthy and exit")
		showVersion   = flag.Bool("version", false, "Show version and exit")
		showHelp      = flag.Bool("h", false, "Show help")
		showHelpLong  = flag.Bool("help", false, "Show help")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Double Agent - SSH Agent Proxy v%s\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <proxy-socket-path>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Arguments:\n")
		fmt.Fprintf(os.Stderr, "  proxy-socket-path    Path to create the proxy socket (e.g., ~/.ssh/agent)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  -v, --verbose        Enable verbose logging\n")
		fmt.Fprintf(os.Stderr, "  -d, --daemon         Run as daemon (detach from terminal)\n")
		fmt.Fprintf(os.Stderr, "  --test-discovery     Test socket discovery and exit\n")
		fmt.Fprintf(os.Stderr, "  --health             Check if proxy is healthy and exit\n")
		fmt.Fprintf(os.Stderr, "  --version            Show version and exit\n")
		fmt.Fprintf(os.Stderr, "  -h, --help           Show this help message\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  # Start proxy in foreground\n")
		fmt.Fprintf(os.Stderr, "  %s ~/.ssh/agent\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Start proxy as daemon\n")
		fmt.Fprintf(os.Stderr, "  %s -d ~/.ssh/agent\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Test socket discovery\n")
		fmt.Fprintf(os.Stderr, "  %s --test-discovery\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Check proxy health\n")
		fmt.Fprintf(os.Stderr, "  %s --health ~/.ssh/agent\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Environment:\n")
		fmt.Fprintf(os.Stderr, "  Set SSH_AUTH_SOCK to the proxy socket path to use it:\n")
		fmt.Fprintf(os.Stderr, "  export SSH_AUTH_SOCK=\"$HOME/.ssh/agent\"\n")
	}

	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Printf("double-agent version %s\n", version)
		os.Exit(0)
	}

	// Handle help flag
	if *showHelp || *showHelpLong {
		flag.Usage()
		os.Exit(0)
	}

	// Combine verbose flags
	verbose = boolPtr(*verbose || *verboseLong)
	daemon = boolPtr(*daemon || *daemonLong)

	// Configure logging
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}
	handler := slog.NewTextHandler(os.Stderr, opts)
	sanitized := proxy.NewSanitizingHandler(handler)
	logger := slog.New(sanitized)

	// Handle test discovery mode
	if *testDiscovery {
		testSocketDiscovery()
		return
	}

	// Handle health check mode
	if *healthCheck {
		if len(flag.Args()) != 1 {
			fmt.Fprintf(os.Stderr, "Error: proxy socket path is required for health check\n\n")
			flag.Usage()
			os.Exit(1)
		}
		proxySocket := expandPath(flag.Args()[0], logger)
		if err := proxy.HealthCheck(proxySocket, logger); err != nil {
			fmt.Printf("Proxy unhealthy: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Proxy is healthy at %s\n", proxySocket)
		os.Exit(0)
	}

	// Check for required argument
	if len(flag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "Error: proxy socket path is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	proxySocket := expandPath(flag.Args()[0], logger)

	// Daemonize if requested
	if *daemon {
		daemonize(proxySocket, *verbose, logger)
		return
	}

	// Run the proxy
	runProxy(proxySocket, logger)
}

func runProxy(proxySocket string, logger *slog.Logger) {
	// Remove existing socket if it exists
	if err := os.Remove(proxySocket); err != nil && !os.IsNotExist(err) {
		logger.Debug("Warning: failed to remove existing socket", "error", err)
	}

	// Create directory if it doesn't exist
	socketDir := filepath.Dir(proxySocket)
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		logger.Error("Failed to create socket directory", "error", err)
		os.Exit(1)
	}

	// Set appropriate permissions
	if err := os.Chmod(proxySocket, 0600); err != nil && !os.IsNotExist(err) {
		logger.Error("Failed to set socket permissions", "error", err)
		os.Exit(1)
	}

	// Create the proxy
	agentProxy := proxy.NewAgentProxy(proxySocket, logger)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Start proxy in a goroutine
	proxyDone := make(chan error, 1)
	go func() {
		proxyDone <- agentProxy.Start()
	}()

	// Print startup message
	logger.Info("Double Agent proxy started", "socket", proxySocket)
	logger.Debug("Process started", "pid", os.Getpid())

	// Wait for shutdown signal or proxy error
	select {
	case sig := <-sigChan:
		logger.Info("Received signal, shutting down", "signal", sig)
	case err := <-proxyDone:
		if err != nil {
			logger.Error("Proxy error", "error", err)
			os.Exit(1)
		}
	}

	// Clean up socket
	_ = os.Remove(proxySocket)
}

func daemonize(proxySocket string, verbose bool, logger *slog.Logger) {
	// Find the executable path
	executable, err := os.Executable()
	if err != nil {
		logger.Error("Failed to find executable", "error", err)
		os.Exit(1)
	}

	// Build arguments for the child process
	args := []string{executable}
	if verbose {
		args = append(args, "-v")
	}
	args = append(args, proxySocket)

	// Start the process detached
	process, err := os.StartProcess(
		executable,
		args,
		&os.ProcAttr{
			Dir:   ".",
			Env:   os.Environ(),
			Files: []*os.File{nil, nil, nil}, // Detach from stdin/stdout/stderr
		},
	)

	if err != nil {
		logger.Error("Failed to start daemon", "error", err)
		os.Exit(1)
	}

	fmt.Printf("Double Agent daemon started (PID: %d)\n", process.Pid)
	fmt.Printf("Socket: %s\n", proxySocket)

	// Release the process so it continues running
	_ = process.Release()
}

func testSocketDiscovery() {
	fmt.Println("Testing SSH agent socket discovery...")
	fmt.Println()

	sockets, err := proxy.DiscoverSockets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Discovery failed: %v\n", err)
		os.Exit(1)
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

func boolPtr(b bool) *bool {
	return &b
}

func expandPath(path string, logger *slog.Logger) string {
	// Expand ~ to home directory
	if len(path) >= 2 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			logger.Error("Failed to get home directory", "error", err)
			os.Exit(1)
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
