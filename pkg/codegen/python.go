package codegen

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// PythonGenerator generates Python requests code from HTTP requests.
type PythonGenerator struct {
	opts Options
}

// NewPythonGenerator creates a new Python code generator.
func NewPythonGenerator(opts Options) *PythonGenerator {
	return &PythonGenerator{opts: opts}
}

// Generate produces Python requests code from the HTTP request.
func (g *PythonGenerator) Generate(req *http.Request) (string, error) {
	var buf bytes.Buffer

	buf.WriteString("import requests\n\n")

	// Prepare headers
	headers := maskRequestHeaders(req.Header, g.opts.MaskSecrets)
	g.writeHeaders(&buf, headers)

	// Prepare body if present
	if req.Body != nil && req.ContentLength != 0 {
		if err := g.writeBody(&buf, req); err != nil {
			return "", err
		}
	}

	// Make request
	g.writeRequest(&buf, req, len(headers) > 0)

	// Print response
	buf.WriteString("print(response.text)\n")

	return buf.String(), nil
}

func (g *PythonGenerator) writeHeaders(buf *bytes.Buffer, headers http.Header) { //nolint:dupl // Python-specific header formatting
	if len(headers) == 0 {
		return
	}

	buf.WriteString("headers = {\n")
	g.writePythonHeaderValues(buf, headers)
	buf.WriteString("}\n\n")
}

// writePythonHeaderValues writes header key-value pairs in Python format.
func (g *PythonGenerator) writePythonHeaderValues(buf *bytes.Buffer, headers http.Header) {
	for key := range headers {
		values := headers[key]
		for _, value := range values {
			buf.WriteString("    '")
			buf.WriteString(key)
			buf.WriteString("': '")
			escapedValue := escapePythonString(value)
			buf.WriteString(escapedValue)
			buf.WriteString("',\n")
		}
	}
}

func (g *PythonGenerator) writeBody(buf *bytes.Buffer, req *http.Request) error {
	body, err := g.readPythonRequestBody(req)
	if err != nil {
		return err
	}

	g.writeDataAssignment(buf, body)
	return nil
}

// readPythonRequestBody reads and masks the request body.
func (g *PythonGenerator) readPythonRequestBody(req *http.Request) (string, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read request body: %w", err)
	}

	// Restore body for potential future use
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	bodyStr := maskBody(body, g.opts.MaskSecrets)
	return bodyStr, nil
}

// writeDataAssignment writes the data assignment line.
func (g *PythonGenerator) writeDataAssignment(buf *bytes.Buffer, bodyStr string) {
	buf.WriteString("data = '''")
	buf.WriteString(bodyStr)
	buf.WriteString("'''\n\n")
}

func (g *PythonGenerator) writeRequest(buf *bytes.Buffer, req *http.Request, hasHeaders bool) {
	g.writeResponseAssignment(buf, req)
	g.writeRequestParameters(buf, req, hasHeaders)
	buf.WriteString(")\n\n")
}

// writeResponseAssignment writes the response assignment line.
func (g *PythonGenerator) writeResponseAssignment(buf *bytes.Buffer, req *http.Request) {
	buf.WriteString("response = requests.")
	buf.WriteString(strings.ToLower(req.Method))
	buf.WriteString("(\n")
	buf.WriteString("    '")
	buf.WriteString(req.URL.String())
	buf.WriteString("',\n")
}

// writeRequestParameters writes the request parameters.
func (g *PythonGenerator) writeRequestParameters(buf *bytes.Buffer, req *http.Request, hasHeaders bool) {
	if hasHeaders {
		buf.WriteString("    headers=headers,\n")
	}

	if req.Body != nil && req.ContentLength != 0 {
		buf.WriteString("    data=data,\n")
	}
}

// escapePythonString escapes special characters for Python string literals.
func escapePythonString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}
