package mcp

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"
)

// freePort reserves an ephemeral port and releases it again, so the test can
// bind a server to a port that was free a moment ago.
func freePort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("failed to release the reserved port: %v", err)
	}
	return strconv.Itoa(port)
}

// TestRunServerSSEShutsDownOnContextCancel proves the SSE transport stops when
// the context is cancelled instead of serving forever.
func TestRunServerSSEShutsDownOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	factory := NewServerFactory("testapp", "1.0")
	server := factory.CreateServer()

	done := make(chan error, 1)
	go func() {
		done <- factory.RunServer(ctx, server, "sse", freePort(t))
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunServer() error = %v, want nil after a graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunServer() did not return after the context was cancelled")
	}
}

// TestRunServerStdioStopsOnContextCancel proves the stdio transport stops when
// the context is cancelled.
func TestRunServerStdioStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	factory := NewServerFactory("testapp", "1.0")
	server := factory.CreateServer()

	done := make(chan error, 1)
	go func() {
		done <- factory.RunServer(ctx, server, "stdio", "8080")
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("RunServer() error = %v, want nil or context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunServer() did not return after the context was cancelled")
	}
}

// TestRunServerRejectsUnknownTransport proves an unknown transport is reported
// instead of silently starting nothing.
func TestRunServerRejectsUnknownTransport(t *testing.T) {
	factory := NewServerFactory("testapp", "1.0")
	server := factory.CreateServer()

	if err := factory.RunServer(context.Background(), server, "carrier-pigeon", "8080"); err == nil {
		t.Fatal("RunServer() expected an error for an unsupported transport")
	}
}
