package cli

import (
	"net/http"
	"strings"
	"testing"
)

type httpErrorCase struct {
	name       string
	statusCode int
	body       []byte
	expected   []string
}

func runHTTPErrorCases(t *testing.T, formatter *ErrorFormatter, tests []httpErrorCase) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Status:     http.StatusText(tt.statusCode),
			}

			result := formatter.FormatHTTPError(resp, tt.body)

			for _, expected := range tt.expected {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected error to contain '%s', got: %s", expected, result)
				}
			}
		})
	}
}
