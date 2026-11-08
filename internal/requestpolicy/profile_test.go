package requestpolicy

import (
	"net/http"
	"testing"

	"github.com/nomagicln/open-bridge/pkg/config"
)

// TestApplyProfilePreservesExistingOverrideRules pins the profile override
// semantics shared by the CLI and both MCP modes: profile headers and query
// parameters override the request, everything else survives.
func TestApplyProfilePreservesExistingOverrideRules(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://user:pass@api.example.test/users?view=brief&keep=one&keep=two", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Tenant", "argument")
	req.Header.Set("X-Keep", "kept")
	profile := &config.Profile{
		Headers:     map[string]string{"X-Tenant": "profile"},
		QueryParams: map[string]string{"view": "full", "tenant": "profile"},
	}

	ApplyProfile(req, profile)

	query := req.URL.Query()
	if query.Get("view") != "full" || query.Get("tenant") != "profile" {
		t.Fatalf("profile query overrides not applied: %s", req.URL.RawQuery)
	}
	if len(query["keep"]) != 2 {
		t.Fatalf("repeated query values not preserved: %s", req.URL.RawQuery)
	}
	if req.Header.Get("X-Tenant") != "profile" {
		t.Errorf("profile header X-Tenant = %q, want profile", req.Header.Get("X-Tenant"))
	}
	if req.Header.Get("X-Keep") != "kept" {
		t.Errorf("unrelated header X-Keep = %q, want kept", req.Header.Get("X-Keep"))
	}
	if req.URL.User == nil || req.URL.User.Username() != "user" {
		t.Errorf("URL userinfo dropped: %v", req.URL.User)
	}
	if password, _ := req.URL.User.Password(); password != "pass" {
		t.Errorf("URL password = %q, want pass", password)
	}
}

// TestApplyProfileKeepsExistingQueryValues documents that a profile only
// overrides the query keys it declares.
func TestApplyProfileKeepsExistingQueryValues(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.example.test/pets?limit=5&page=1", nil)
	if err != nil {
		t.Fatal(err)
	}

	ApplyProfile(req, &config.Profile{QueryParams: map[string]string{"limit": "10"}})

	query := req.URL.Query()
	if query.Get("limit") != "10" {
		t.Errorf("profile query limit = %q, want 10 (profile overrides)", query.Get("limit"))
	}
	if query.Get("page") != "1" {
		t.Errorf("existing query page = %q, want 1 (preserved)", query.Get("page"))
	}
}

// TestApplyProfileToleratesNilInputs ensures the helper is defensive.
func TestApplyProfileToleratesNilInputs(t *testing.T) {
	ApplyProfile(nil, &config.Profile{})
	req, err := http.NewRequest(http.MethodGet, "https://api.example.test/pets", nil)
	if err != nil {
		t.Fatal(err)
	}
	ApplyProfile(req, nil)
	if req.URL == nil {
		t.Fatal("request URL unexpectedly nil")
	}
}
