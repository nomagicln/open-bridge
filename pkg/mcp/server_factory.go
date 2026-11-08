package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// sseReadHeaderTimeout bounds how long the SSE server waits for request
	// headers. It does not bound the lifetime of a streaming response, so long
	// lived SSE connections keep working.
	sseReadHeaderTimeout = 10 * time.Second

	// sseShutdownTimeout bounds how long the SSE server waits for in-flight
	// requests to finish when the context is cancelled.
	sseShutdownTimeout = 5 * time.Second
)

// ServerFactory creates and manages MCP servers.
type ServerFactory struct {
	Impl *mcp.Implementation
}

// NewServerFactory creates a new server factory.
func NewServerFactory(name, version string) *ServerFactory {
	return &ServerFactory{
		Impl: &mcp.Implementation{
			Name:    name,
			Version: version,
		},
	}
}

// CreateServer creates a new MCP server instance.
func (f *ServerFactory) CreateServer() *mcp.Server {
	return mcp.NewServer(f.Impl, &mcp.ServerOptions{})
}

// RunServer runs the server with the specified transport.
//
// The server stops when the context is cancelled: the stdio transport is
// stopped by the SDK, the SSE transport is shut down gracefully.
func (f *ServerFactory) RunServer(ctx context.Context, server *mcp.Server, transport string, port string) error {
	switch transport {
	case "stdio":
		return server.Run(ctx, &mcp.StdioTransport{})
	case "sse":
		return f.runSSEServer(ctx, server, port)
	default:
		return fmt.Errorf("unsupported transport: %s", transport)
	}
}

// runSSEServer serves the SSE transport until the context is cancelled.
func (f *ServerFactory) runSSEServer(ctx context.Context, server *mcp.Server, port string) error {
	sseHandler := mcp.NewSSEHandler(func(r *http.Request) *mcp.Server {
		return server
	}, nil)

	addr := ":" + port
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           sseHandler,
		ReadHeaderTimeout: sseReadHeaderTimeout,
	}

	fmt.Fprintf(os.Stderr, "Starting SSE server on %s\n", addr)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), sseShutdownTimeout)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shut down SSE server: %w", err)
		}
		return nil
	}
}
