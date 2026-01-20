package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
func (f *ServerFactory) RunServer(ctx context.Context, server *mcp.Server, transport string, port string) error {
	switch transport {
	case "stdio":
		return server.Run(ctx, &mcp.StdioTransport{})
	case "sse":
		sseHandler := mcp.NewSSEHandler(func(r *http.Request) *mcp.Server {
			return server
		}, nil)

		addr := ":" + port
		fmt.Fprintf(os.Stderr, "Starting SSE server on %s\n", addr)
		return http.ListenAndServe(addr, sseHandler)
	default:
		return fmt.Errorf("unsupported transport: %s", transport)
	}
}
