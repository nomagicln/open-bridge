// Package mcp provides MCP (Model Context Protocol) server functionality.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// Server represents an MCP server that listens for JSON-RPC requests.
type Server struct {
	handler *Handler
	in      io.Reader
	out     io.Writer
}

// NewServer creates a new MCP server.
func NewServer(handler *Handler, in io.Reader, out io.Writer) *Server {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return &Server{
		handler: handler,
		in:      in,
		out:     out,
	}
}

// Serve starts the server loop, reading requests and writing responses.
// It blocks until the context is canceled or the input stream is closed.
func (s *Server) Serve(ctx context.Context) error {
	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	scanner := bufio.NewScanner(s.in)

	// Default buffer size might be too small for large requests (e.g. creating resources with large bodies)
	// increasing to 1MB
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	// Output error channel
	errChan := make(chan error, 1)

	go func() {
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			// Process request
			resp, err := s.handler.HandleRequest(line)
			if err != nil {
				// Protocol error
				s.writeError("ParseError", err.Error())
				continue
			}

			// Write response
			if err := s.writeResponse(resp); err != nil {
				errChan <- fmt.Errorf("failed to write response: %w", err)
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("scanner error: %w", err)
		}
		close(errChan)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errChan:
		return err
	}
}

// writeResponse writes a JSON-RPC response to the output stream.
func (s *Server) writeResponse(resp *Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	// Lock stdout if needed, but for now assuming single writer
	_, err = fmt.Fprintf(s.out, "%s\n", data)
	return err
}

// writeError writes a raw error response for protocol failures.
func (s *Server) writeError(code, message string) {
	errResp := map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    -32700, // Parse error
			"message": message,
		},
		"id": nil,
	}
	data, _ := json.Marshal(errResp)
	_, _ = fmt.Fprintf(s.out, "%s\n", data)
}
