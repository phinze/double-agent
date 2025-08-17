package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// BenchmarkSocketDiscovery benchmarks the socket discovery process
func BenchmarkSocketDiscovery(b *testing.B) {
	// Create some mock sockets to discover
	tmpDir := b.TempDir()
	
	for i := 0; i < 5; i++ {
		sshDir := filepath.Join(tmpDir, fmt.Sprintf("ssh-test%d", i))
		os.MkdirAll(sshDir, 0700)
		socketPath := filepath.Join(sshDir, fmt.Sprintf("agent.%d", i))
		
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			b.Fatalf("Failed to create test socket: %v", err)
		}
		defer listener.Close()
		
		// Simple handler for benchmark
		go func(l net.Listener) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					buf := make([]byte, 5)
					if n, _ := c.Read(buf); n == 5 && buf[4] == SSH_AGENTC_REQUEST_IDENTITIES {
						response := []byte{0, 0, 0, 5, SSH_AGENT_IDENTITIES_ANSWER, 0, 0, 0, 0}
						c.Write(response)
					}
				}(conn)
			}
		}(listener)
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_, _ = FindActiveSocket()
	}
}

// BenchmarkProxyThroughput benchmarks data throughput through the proxy
func BenchmarkProxyThroughput(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	// Create mock agent
	agentSocket := createHighPerformanceMockAgent(b)
	defer os.Remove(agentSocket)
	
	// Start proxy with cached agent
	tmpDir := b.TempDir()
	proxySocket := filepath.Join(tmpDir, "proxy.sock")
	
	ap := NewAgentProxy(proxySocket, logger)
	ap.activeSocket = agentSocket
	ap.lastCheck = time.Now()
	go ap.Start()
	time.Sleep(50 * time.Millisecond)
	
	// Prepare request
	request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
	response := make([]byte, 9)
	
	b.ResetTimer()
	b.SetBytes(int64(len(request) + len(response)))
	
	for i := 0; i < b.N; i++ {
		conn, err := net.Dial("unix", proxySocket)
		if err != nil {
			b.Fatalf("Failed to connect: %v", err)
		}
		
		if _, err := conn.Write(request); err != nil {
			conn.Close()
			b.Fatalf("Failed to write: %v", err)
		}
		
		if _, err := io.ReadFull(conn, response); err != nil {
			conn.Close()
			b.Fatalf("Failed to read: %v", err)
		}
		
		conn.Close()
	}
	
	os.Remove(proxySocket)
}

// BenchmarkConcurrentConnections benchmarks handling of concurrent connections
func BenchmarkConcurrentConnections(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	// Create mock agent
	agentSocket := createHighPerformanceMockAgent(b)
	defer os.Remove(agentSocket)
	
	// Start proxy with cached agent
	tmpDir := b.TempDir()
	proxySocket := filepath.Join(tmpDir, "proxy.sock")
	
	ap := NewAgentProxy(proxySocket, logger)
	ap.activeSocket = agentSocket
	ap.lastCheck = time.Now()
	go ap.Start()
	time.Sleep(50 * time.Millisecond)
	
	b.ResetTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
		response := make([]byte, 9)
		
		for pb.Next() {
			conn, err := net.Dial("unix", proxySocket)
			if err != nil {
				b.Fatalf("Failed to connect: %v", err)
			}
			
			if _, err := conn.Write(request); err != nil {
				conn.Close()
				b.Fatalf("Failed to write: %v", err)
			}
			
			if _, err := io.ReadFull(conn, response); err != nil {
				conn.Close()
				b.Fatalf("Failed to read: %v", err)
			}
			
			conn.Close()
		}
	})
	
	os.Remove(proxySocket)
}

// BenchmarkCachePerformance benchmarks the caching mechanism
func BenchmarkCachePerformance(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ap := NewAgentProxy("/tmp/test.sock", logger)
	
	// Create a mock socket that's always valid
	testSocket := createHighPerformanceMockAgent(b)
	defer os.Remove(testSocket)
	
	// Pre-populate cache
	ap.activeSocket = testSocket
	ap.lastCheck = time.Now()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_ = ap.FindActiveSocketCached()
	}
}

// BenchmarkFailover benchmarks failover performance
func BenchmarkFailover(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	// Create two mock agents
	agent1 := createHighPerformanceMockAgent(b)
	agent2 := createHighPerformanceMockAgent(b)
	defer os.Remove(agent1)
	defer os.Remove(agent2)
	
	// Start proxy
	tmpDir := b.TempDir()
	proxySocket := filepath.Join(tmpDir, "proxy.sock")
	
	ap := NewAgentProxy(proxySocket, logger)
	ap.activeSocket = agent1
	ap.lastCheck = time.Now()
	go ap.Start()
	time.Sleep(50 * time.Millisecond)
	
	// Alternate between agents to simulate failover
	useAgent1 := true
	
	request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
	response := make([]byte, 9)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		// Switch agents every 10 requests to trigger cache invalidation
		if i%10 == 0 {
			useAgent1 = !useAgent1
			if useAgent1 {
				ap.activeSocket = agent1
			} else {
				ap.activeSocket = agent2
			}
			ap.lastCheck = time.Now().Add(-10 * time.Second) // Force re-validation
		}
		
		conn, err := net.Dial("unix", proxySocket)
		if err != nil {
			b.Fatalf("Failed to connect: %v", err)
		}
		
		if _, err := conn.Write(request); err != nil {
			conn.Close()
			b.Fatalf("Failed to write: %v", err)
		}
		
		if _, err := io.ReadFull(conn, response); err != nil {
			conn.Close()
			b.Fatalf("Failed to read: %v", err)
		}
		
		conn.Close()
	}
	
	os.Remove(proxySocket)
}

// BenchmarkLogSanitization benchmarks the log sanitization overhead
func BenchmarkLogSanitization(b *testing.B) {
	// Benchmark without sanitization
	b.Run("WithoutSanitization", func(b *testing.B) {
		handler := slog.NewTextHandler(io.Discard, nil)
		logger := slog.New(handler)
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("Connection established",
				"socket", "/home/testuser/.ssh/agent",
				"fingerprint", "SHA256:abcdef123456",
				"pid", 12345)
		}
	})
	
	// Benchmark with sanitization
	b.Run("WithSanitization", func(b *testing.B) {
		handler := slog.NewTextHandler(io.Discard, nil)
		sanitized := NewSanitizingHandler(handler)
		logger := slog.New(sanitized)
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("Connection established",
				"socket", "/home/testuser/.ssh/agent",
				"fingerprint", "SHA256:abcdef123456",
				"pid", 12345)
		}
	})
}

// BenchmarkMemoryUsage tracks memory allocations
func BenchmarkMemoryUsage(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	// Create mock agent
	agentSocket := createHighPerformanceMockAgent(b)
	defer os.Remove(agentSocket)
	
	// Create proxy with cached agent
	ap := NewAgentProxy("/tmp/test.sock", logger)
	ap.activeSocket = agentSocket
	ap.lastCheck = time.Now()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		client, proxyEnd := net.Pipe()
		
		go ap.HandleConnection(proxyEnd)
		
		// Send request
		request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
		client.Write(request)
		
		// Read response
		response := make([]byte, 9)
		io.ReadFull(client, response)
		
		client.Close()
	}
}

// createHighPerformanceMockAgent creates an optimized mock agent for benchmarking
func createHighPerformanceMockAgent(b *testing.B) string {
	tmpDir := b.TempDir()
	socketPath := filepath.Join(tmpDir, "perf-agent.sock")
	
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		b.Fatalf("Failed to create mock agent: %v", err)
	}
	
	// Pre-create response
	response := []byte{0, 0, 0, 5, SSH_AGENT_IDENTITIES_ANSWER, 0, 0, 0, 0}
	
	// Use a worker pool for handling connections
	workerCount := 10
	var wg sync.WaitGroup
	wg.Add(workerCount)
	
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			buf := make([]byte, 1024)
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				
				// Fast path for known request
				n, err := conn.Read(buf)
				if err == nil && n >= 5 && buf[4] == SSH_AGENTC_REQUEST_IDENTITIES {
					conn.Write(response)
				}
				conn.Close()
			}
		}()
	}
	
	return socketPath
}

// Latency distribution benchmark
func BenchmarkLatencyDistribution(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	
	// Create mock agent
	agentSocket := createHighPerformanceMockAgent(b)
	defer os.Remove(agentSocket)
	
	// Start proxy with cached agent
	tmpDir := b.TempDir()
	proxySocket := filepath.Join(tmpDir, "proxy.sock")
	
	ap := NewAgentProxy(proxySocket, logger)
	ap.activeSocket = agentSocket
	ap.lastCheck = time.Now()
	go ap.Start()
	time.Sleep(50 * time.Millisecond)
	
	// Measure individual request latencies
	latencies := make([]time.Duration, b.N)
	request := []byte{0, 0, 0, 1, SSH_AGENTC_REQUEST_IDENTITIES}
	response := make([]byte, 9)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		start := time.Now()
		
		conn, err := net.Dial("unix", proxySocket)
		if err != nil {
			b.Fatalf("Failed to connect: %v", err)
		}
		
		if _, err := conn.Write(request); err != nil {
			conn.Close()
			b.Fatalf("Failed to write: %v", err)
		}
		
		if _, err := io.ReadFull(conn, response); err != nil {
			conn.Close()
			b.Fatalf("Failed to read: %v", err)
		}
		
		conn.Close()
		
		latencies[i] = time.Since(start)
	}
	
	// Calculate percentiles
	if len(latencies) > 0 {
		// Sort for percentile calculation
		sortedLatencies := make([]time.Duration, len(latencies))
		copy(sortedLatencies, latencies)
		
		// Simple bubble sort for small datasets
		for i := 0; i < len(sortedLatencies); i++ {
			for j := i + 1; j < len(sortedLatencies); j++ {
				if sortedLatencies[i] > sortedLatencies[j] {
					sortedLatencies[i], sortedLatencies[j] = sortedLatencies[j], sortedLatencies[i]
				}
			}
		}
		
		p50 := sortedLatencies[len(sortedLatencies)*50/100]
		p95 := sortedLatencies[len(sortedLatencies)*95/100]
		p99 := sortedLatencies[len(sortedLatencies)*99/100]
		
		b.Logf("Latency - P50: %v, P95: %v, P99: %v", p50, p95, p99)
	}
	
	os.Remove(proxySocket)
}