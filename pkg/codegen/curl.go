package codegen

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// CurlGenerator generates curl command code from HTTP requests.
type CurlGenerator struct {
	opts Options
}

// NewCurlGenerator creates a new curl code generator.
func NewCurlGenerator(opts Options) *CurlGenerator {
	return &CurlGenerator{opts: opts}
}

// Generate produces a curl command from the HTTP request.
func (g *CurlGenerator) Generate(req *http.Request) (string, error) {
	var buf bytes.Buffer

	// Start with curl command
	buf.WriteString("curl -X ")
	buf.WriteString(req.Method)

	// Add URL
	buf.WriteString(" '")
	buf.WriteString(req.URL.String())
	buf.WriteString("'")

	// Get masked headers
	headers := maskRequestHeaders(req.Header, g.opts.MaskSecrets)

	// Add headers
	for key := range headers {
		values := headers[key]
		for _, value := range values {
			buf.WriteString(" \\\n  -H '")
			buf.WriteString(key)
			buf.WriteString(": ")
			buf.WriteString(value)
			buf.WriteString("'")
		}
	}

	// Add body if present
	if req.Body != nil && req.ContentLength != 0 {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read request body: %w", err)
		}

		// Restore body for potential future use
		req.Body = io.NopCloser(bytes.NewBuffer(body))

		bodyStr := maskBody(body, g.opts.MaskSecrets)
		buf.WriteString(" \\\n  -d '")
		// Escape single quotes in the body
		bodyStr = strings.ReplaceAll(bodyStr, "'", "'\\''")
		buf.WriteString(bodyStr)
		buf.WriteString("'")
	}

	buf.WriteString("\n")
	return buf.String(), nil
}
