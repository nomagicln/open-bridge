package codegen

import (
	"bytes"
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

	g.writeCurlCommand(&buf, req)
	g.writeURL(&buf, req)
	g.writeHeaders(&buf, req)
	g.writeBody(&buf, req)

	return buf.String(), nil
}

// writeCurlCommand writes the curl command with method.
func (g *CurlGenerator) writeCurlCommand(buf *bytes.Buffer, req *http.Request) {
	buf.WriteString("curl -X ")
	buf.WriteString(req.Method)
}

// writeURL writes the URL to the buffer.
func (g *CurlGenerator) writeURL(buf *bytes.Buffer, req *http.Request) {
	buf.WriteString(" '")
	buf.WriteString(req.URL.String())
	buf.WriteString("'")
}

// writeHeaders writes all headers to the buffer.
func (g *CurlGenerator) writeHeaders(buf *bytes.Buffer, req *http.Request) {
	headers := maskRequestHeaders(req.Header, g.opts.MaskSecrets)

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
}

// writeBody writes the request body to the buffer if present.
func (g *CurlGenerator) writeBody(buf *bytes.Buffer, req *http.Request) {
	if req.Body == nil || req.ContentLength == 0 {
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return
	}

	// Restore body for potential future use
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	bodyStr := maskBody(body, g.opts.MaskSecrets)
	buf.WriteString(" \\\n  -d '")
	bodyStr = strings.ReplaceAll(bodyStr, "'", "'\\''")
	buf.WriteString(bodyStr)
	buf.WriteString("'")
}
