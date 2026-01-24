package codegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// NodeJSGenerator generates Node.js fetch API code from HTTP requests.
type NodeJSGenerator struct {
	opts Options
}

// NewNodeJSGenerator creates a new Node.js code generator.
func NewNodeJSGenerator(opts Options) *NodeJSGenerator {
	return &NodeJSGenerator{opts: opts}
}

// Generate produces Node.js fetch code from the HTTP request.
func (g *NodeJSGenerator) Generate(req *http.Request) (string, error) {
	var buf bytes.Buffer

	buf.WriteString("const response = await fetch('")
	buf.WriteString(req.URL.String())
	buf.WriteString("', {\n")

	// Method
	if req.Method != "GET" {
		buf.WriteString("  method: '")
		buf.WriteString(req.Method)
		buf.WriteString("',\n")
	}

	// Headers
	headers := maskRequestHeaders(req.Header, g.opts.MaskSecrets)
	g.writeHeaders(&buf, headers)

	// Body
	if req.Body != nil && req.ContentLength != 0 {
		if err := g.writeBody(&buf, req); err != nil {
			return "", err
		}
	}

	buf.WriteString("});\n\n")
	buf.WriteString("const data = await response.json();\n")
	buf.WriteString("console.log(data);\n")

	return buf.String(), nil
}

func (g *NodeJSGenerator) writeHeaders(buf *bytes.Buffer, headers http.Header) { //nolint:dupl // JavaScript-specific header formatting
	if len(headers) == 0 {
		return
	}

	buf.WriteString("  headers: {\n")
	for key := range headers {
		values := headers[key]
		for _, value := range values {
			buf.WriteString("    '")
			buf.WriteString(key)
			buf.WriteString("': '")
			escapedValue := escapeString(value)
			buf.WriteString(escapedValue)
			buf.WriteString("',\n")
		}
	}
	buf.WriteString("  },\n")
}

func (g *NodeJSGenerator) writeBody(buf *bytes.Buffer, req *http.Request) error {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}

	// Restore body for potential future use
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	bodyStr := maskBody(body, g.opts.MaskSecrets)

	// Try to parse as JSON for better formatting
	var jsonData any
	if err := json.Unmarshal(body, &jsonData); err == nil {
		// Valid JSON, format it nicely
		buf.WriteString("  body: JSON.stringify(")
		jsonBytes, _ := json.MarshalIndent(jsonData, "    ", "  ")
		buf.WriteString(string(jsonBytes))
		buf.WriteString("),\n")
	} else {
		// Not JSON, treat as string
		buf.WriteString("  body: '")
		escaped := escapeString(bodyStr)
		buf.WriteString(escaped)
		buf.WriteString("',\n")
	}
	return nil
}

// escapeString escapes special characters for JavaScript string literals.
func escapeString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}
