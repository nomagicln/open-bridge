package codegen

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// GoGenerator generates Go net/http code from HTTP requests.
type GoGenerator struct {
	opts Options
}

// NewGoGenerator creates a new Go code generator.
func NewGoGenerator(opts Options) *GoGenerator {
	return &GoGenerator{opts: opts}
}

// Generate produces Go net/http code from the HTTP request.
func (g *GoGenerator) Generate(req *http.Request) (string, error) {
	var buf bytes.Buffer

	// Package and imports
	buf.WriteString("package main\n\n")
	hasBody := req.Body != nil && req.ContentLength != 0
	g.writeImports(&buf, hasBody)

	// Main function
	buf.WriteString("func main() {\n")

	// Create request
	if hasBody {
		if err := g.writeBodyRequest(&buf, req); err != nil {
			return "", err
		}
	} else {
		buf.WriteString("	req, err := http.NewRequest(\"")
		buf.WriteString(req.Method)
		buf.WriteString("\", \"")
		buf.WriteString(req.URL.String())
		buf.WriteString("\", nil)\n")
	}

	buf.WriteString("	if err != nil {\n")
	buf.WriteString("		fmt.Println(err)\n")
	buf.WriteString("		return\n")
	buf.WriteString("	}\n\n")

	// Add headers
	headers := maskRequestHeaders(req.Header, g.opts.MaskSecrets)
	g.writeHeaders(&buf, headers)

	// Make request and read response
	g.writeExecuteRequest(&buf)

	return buf.String(), nil
}

func (g *GoGenerator) writeImports(buf *bytes.Buffer, hasBody bool) {
	buf.WriteString("import (\n")
	buf.WriteString("	\"fmt\"\n")
	buf.WriteString("	\"io\"\n")
	buf.WriteString("	\"net/http\"\n")
	if hasBody {
		buf.WriteString("	\"strings\"\n")
	}
	buf.WriteString(")\n\n")
}

func (g *GoGenerator) writeBodyRequest(buf *bytes.Buffer, req *http.Request) error {
	body, err := g.readAndMaskBody(req)
	if err != nil {
		return err
	}

	g.writePayloadCreation(buf, body)
	g.writeRequestCreation(buf, req, "payload")

	return nil
}

// readAndMaskBody reads and masks the request body.
func (g *GoGenerator) readAndMaskBody(req *http.Request) (string, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read request body: %w", err)
	}

	// Restore body for potential future use
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	bodyStr := maskBody(body, g.opts.MaskSecrets)
	return escapeBackticks(bodyStr), nil
}

// escapeBackticks escapes backticks in the body string.
func escapeBackticks(bodyStr string) string {
	return strings.ReplaceAll(bodyStr, "`", "` + \"`\" + `")
}

// writePayloadCreation writes the payload creation line.
func (g *GoGenerator) writePayloadCreation(buf *bytes.Buffer, bodyStr string) {
	buf.WriteString("	payload := strings.NewReader(`")
	buf.WriteString(bodyStr)
	buf.WriteString("`)\n\n")
}

// writeRequestCreation writes the request creation line.
func (g *GoGenerator) writeRequestCreation(buf *bytes.Buffer, req *http.Request, payloadVar string) {
	buf.WriteString("	req, err := http.NewRequest(\"")
	buf.WriteString(req.Method)
	buf.WriteString("\", \"")
	buf.WriteString(req.URL.String())
	buf.WriteString("\", ")
	buf.WriteString(payloadVar)
	buf.WriteString(")\n")
}

func (g *GoGenerator) writeHeaders(buf *bytes.Buffer, headers http.Header) {
	for key := range headers {
		values := headers[key]
		for _, value := range values {
			buf.WriteString("	req.Header.Add(\"")
			buf.WriteString(key)
			buf.WriteString("\", \"")
			escapedValue := escapeGoString(value)
			buf.WriteString(escapedValue)
			buf.WriteString("\")\n")
		}
	}

	if len(headers) > 0 {
		buf.WriteString("\n")
	}
}

func (g *GoGenerator) writeExecuteRequest(buf *bytes.Buffer) {
	buf.WriteString("	res, err := http.DefaultClient.Do(req)\n")
	buf.WriteString("	if err != nil {\n")
	buf.WriteString("		fmt.Println(err)\n")
	buf.WriteString("		return\n")
	buf.WriteString("	}\n")
	buf.WriteString("	defer res.Body.Close()\n\n")

	buf.WriteString("	body, err := io.ReadAll(res.Body)\n")
	buf.WriteString("	if err != nil {\n")
	buf.WriteString("		fmt.Println(err)\n")
	buf.WriteString("		return\n")
	buf.WriteString("	}\n\n")

	buf.WriteString("	fmt.Println(string(body))\n")
	buf.WriteString("}\n")
}

// escapeGoString escapes special characters for Go string literals.
func escapeGoString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}
