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
	g.writeHeaderValues(buf, headers)
	buf.WriteString("  },\n")
}

// writeHeaderValues writes header key-value pairs.
func (g *NodeJSGenerator) writeHeaderValues(buf *bytes.Buffer, headers http.Header) {
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
}

func (g *NodeJSGenerator) writeBody(buf *bytes.Buffer, req *http.Request) error {
	body, err := readRequestBody(req)
	if err != nil {
		return err
	}

	bodyStr := maskBody(body, g.opts.MaskSecrets)

	if isJSON(body) {
		g.writeJSONBody(buf, body)
	} else {
		g.writeStringBody(buf, bodyStr)
	}

	return nil
}

// readRequestBody reads and restores the request body.
func readRequestBody(req *http.Request) ([]byte, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	// Restore body for potential future use
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	return body, nil
}

// isJSON checks if the body content is valid JSON.
func isJSON(body []byte) bool {
	var jsonData any
	return json.Unmarshal(body, &jsonData) == nil
}

// writeJSONBody writes a formatted JSON body.
func (g *NodeJSGenerator) writeJSONBody(buf *bytes.Buffer, body []byte) {
	var jsonData any
	_ = json.Unmarshal(body, &jsonData)

	buf.WriteString("  body: JSON.stringify(")
	jsonBytes, _ := json.MarshalIndent(jsonData, "    ", "  ")
	buf.WriteString(string(jsonBytes))
	buf.WriteString("),\n")
}

// writeStringBody writes a string body.
func (g *NodeJSGenerator) writeStringBody(buf *bytes.Buffer, bodyStr string) {
	buf.WriteString("  body: '")
	escaped := escapeString(bodyStr)
	buf.WriteString(escaped)
	buf.WriteString("',\n")
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
