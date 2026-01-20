// Package main provides performance benchmarks for OpenBridge.
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nomagicln/open-bridge/pkg/mcp"
)

const (
	// mcpListToolsRequest is a standard JSON-RPC request for listing MCP tools
	mcpListToolsRequest = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
)

// getBinaryName returns the platform-specific binary name
func getBinaryName(baseName string) string {
	if runtime.GOOS == "windows" {
		return baseName + ".exe"
	}
	return baseName
}

// BenchmarkColdStart measures the cold start performance of the CLI
func BenchmarkColdStart(b *testing.B) {
	// Build the binary first
	binPath := filepath.Join(b.TempDir(), getBinaryName("ob"))
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	if err := cmd.Run(); err != nil {
		b.Fatalf("Failed to build binary: %v", err)
	}

	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		start := time.Now()
		cmd := exec.Command(binPath, "--version")
		if err := cmd.Run(); err != nil {
			b.Fatalf("Failed to run binary: %v", err)
		}
		elapsed := time.Since(start)
		
		// Log individual run time
		b.Logf("Run %d: %v", i+1, elapsed)
		
		// Fail if any single run exceeds 100ms
		if elapsed > 100*time.Millisecond {
			b.Errorf("Cold start took %v, exceeds requirement of 100ms", elapsed)
		}
	}
}

// TestColdStartPerformance measures actual cold start performance with realistic conditions
func TestColdStartPerformance(t *testing.T) {
	// Build the binary
	binPath := filepath.Join(t.TempDir(), getBinaryName("ob"))
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	// Run multiple iterations to get average
	const iterations = 10
	var total time.Duration
	var max time.Duration
	
	for i := 0; i < iterations; i++ {
		start := time.Now()
		cmd := exec.Command(binPath, "--version")
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to run binary: %v", err)
		}
		elapsed := time.Since(start)
		total += elapsed
		if elapsed > max {
			max = elapsed
		}
		t.Logf("Iteration %d: %v", i+1, elapsed)
	}
	
	avg := total / iterations
	t.Logf("\nCold Start Performance:")
	t.Logf("  Average: %v", avg)
	t.Logf("  Maximum: %v", max)
	t.Logf("  Target:  < 100ms")
	
	if avg > 100*time.Millisecond {
		t.Errorf("Average cold start time %v exceeds requirement of 100ms", avg)
	}
}

// TestMCPServerStartup measures MCP server startup performance
func TestMCPServerStartup(t *testing.T) {
	// Create a minimal test setup
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		t.Fatalf("Failed to create temp config dir: %v", err)
	}
	
	// Set config dir
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("HOME", tempDir)
	
	const iterations = 10
	var total time.Duration
	var max time.Duration
	
	for i := 0; i < iterations; i++ {
		start := time.Now()
		
		// Create a mock handler (the actual initialization is what we're measuring)
		handler := mcp.NewHandler(nil, nil, nil)
		
		// Create server
		in := bytes.NewBuffer(nil)
		out := bytes.NewBuffer(nil)
		server := mcp.NewServer(handler, in, out)
		if server == nil {
			t.Fatal("Failed to create MCP server")
		}
		
		elapsed := time.Since(start)
		total += elapsed
		if elapsed > max {
			max = elapsed
		}
		t.Logf("Iteration %d: %v", i+1, elapsed)
	}
	
	avg := total / iterations
	t.Logf("\nMCP Server Startup Performance:")
	t.Logf("  Average: %v", avg)
	t.Logf("  Maximum: %v", max)
	t.Logf("  Target:  < 200ms")
	
	if avg > 200*time.Millisecond {
		t.Errorf("Average MCP server startup time %v exceeds requirement of 200ms", avg)
	}
}

// BenchmarkMCPListTools measures list_tools response time
func BenchmarkMCPListTools(b *testing.B) {
	// Create a handler with a simple spec
	handler := mcp.NewHandler(nil, nil, nil)
	
	// Create a list_tools request
	request := []byte(mcpListToolsRequest)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		start := time.Now()
		resp, err := handler.HandleRequest(request)
		elapsed := time.Since(start)
		
		if err != nil {
			b.Fatalf("HandleRequest failed: %v", err)
		}
		if resp == nil {
			b.Fatal("Response is nil")
		}
		
		b.Logf("Run %d: %v", i+1, elapsed)
		
		// Fail if any single run exceeds 50ms
		if elapsed > 50*time.Millisecond {
			b.Errorf("list_tools took %v, exceeds requirement of 50ms", elapsed)
		}
	}
}

// TestMCPListToolsPerformance measures actual list_tools performance
func TestMCPListToolsPerformance(t *testing.T) {
	// Create a handler
	handler := mcp.NewHandler(nil, nil, nil)
	
	// Create a list_tools request
	request := []byte(mcpListToolsRequest)
	
	const iterations = 100
	var total time.Duration
	var max time.Duration
	
	for i := 0; i < iterations; i++ {
		start := time.Now()
		resp, err := handler.HandleRequest(request)
		elapsed := time.Since(start)
		
		if err != nil {
			t.Fatalf("HandleRequest failed: %v", err)
		}
		if resp == nil {
			t.Fatal("Response is nil")
		}
		
		total += elapsed
		if elapsed > max {
			max = elapsed
		}
		
		if i < 10 {
			t.Logf("Iteration %d: %v", i+1, elapsed)
		}
	}
	
	avg := total / iterations
	t.Logf("\nMCP list_tools Performance:")
	t.Logf("  Average: %v", avg)
	t.Logf("  Maximum: %v", max)
	t.Logf("  Target:  < 50ms")
	
	if avg > 50*time.Millisecond {
		t.Errorf("Average list_tools response time %v exceeds requirement of 50ms", avg)
	}
}

// TestFullMCPServerPerformance tests the full MCP server workflow
func TestFullMCPServerPerformance(t *testing.T) {
	// This test measures the time from server creation to handling the first request
	const iterations = 10
	var totalStartup time.Duration
	var totalListTools time.Duration
	var maxStartup time.Duration
	var maxListTools time.Duration
	
	for i := 0; i < iterations; i++ {
		// Measure server startup
		startupStart := time.Now()
		handler := mcp.NewHandler(nil, nil, nil)
		in := bytes.NewBuffer(nil)
		out := bytes.NewBuffer(nil)
		server := mcp.NewServer(handler, in, out)
		if server == nil {
			t.Fatal("Failed to create MCP server")
		}
		startupElapsed := time.Since(startupStart)
		
		totalStartup += startupElapsed
		if startupElapsed > maxStartup {
			maxStartup = startupElapsed
		}
		
		// Measure list_tools
		request := []byte(mcpListToolsRequest)
		listToolsStart := time.Now()
		resp, err := handler.HandleRequest(request)
		listToolsElapsed := time.Since(listToolsStart)
		
		if err != nil {
			t.Fatalf("HandleRequest failed: %v", err)
		}
		if resp == nil {
			t.Fatal("Response is nil")
		}
		
		totalListTools += listToolsElapsed
		if listToolsElapsed > maxListTools {
			maxListTools = listToolsElapsed
		}
		
		t.Logf("Iteration %d: startup=%v, list_tools=%v", i+1, startupElapsed, listToolsElapsed)
	}
	
	avgStartup := totalStartup / iterations
	avgListTools := totalListTools / iterations
	
	t.Logf("\nFull MCP Server Performance:")
	t.Logf("  Startup - Average: %v, Max: %v (Target: < 200ms)", avgStartup, maxStartup)
	t.Logf("  list_tools - Average: %v, Max: %v (Target: < 50ms)", avgListTools, maxListTools)
	
	if avgStartup > 200*time.Millisecond {
		t.Errorf("Average MCP server startup time %v exceeds requirement of 200ms", avgStartup)
	}
	
	if avgListTools > 50*time.Millisecond {
		t.Errorf("Average list_tools response time %v exceeds requirement of 50ms", avgListTools)
	}
}
