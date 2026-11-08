// Package requestpolicy applies shared request policy at the CLI/MCP boundary.
package requestpolicy

import (
	"net/http"

	"github.com/nomagicln/open-bridge/pkg/config"
)

// ApplyProfile layers profile-level headers and query parameters onto the
// request. Existing headers are kept unless overridden, existing query values
// and URL userinfo are preserved.
//
// This is the single definition of profile override semantics: the CLI and both
// MCP modes apply profiles identically.
func ApplyProfile(req *http.Request, profile *config.Profile) {
	if req == nil || profile == nil {
		return
	}

	for name, value := range profile.Headers {
		req.Header.Set(name, value)
	}

	if req.URL == nil || len(profile.QueryParams) == 0 {
		return
	}

	query := req.URL.Query()
	for name, value := range profile.QueryParams {
		query.Set(name, value)
	}
	req.URL.RawQuery = query.Encode()
}
