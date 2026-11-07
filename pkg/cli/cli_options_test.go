package cli

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/nomagicln/open-bridge/pkg/config"
)

// TestParseCLIOptions covers the CLI-only option parser: long/short forms,
// = and space separated values, output format priority and `--` delimiter
// compatibility with request-parameter parsing.
func TestParseCLIOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want cliOptions
	}{
		{
			name: "no args",
			args: nil,
			want: cliOptions{},
		},
		{
			name: "output long space separated",
			args: []string{"--output", "json"},
			want: cliOptions{outputFormat: "json"},
		},
		{
			name: "output long equals separated",
			args: []string{"--output=yaml"},
			want: cliOptions{outputFormat: "yaml"},
		},
		{
			name: "output short space separated",
			args: []string{"-o", "json"},
			want: cliOptions{outputFormat: "json"},
		},
		{
			name: "output short equals separated",
			args: []string{"-o=json"},
			want: cliOptions{outputFormat: "json"},
		},
		{
			name: "json shorthand",
			args: []string{"--json"},
			want: cliOptions{outputFormat: "json"},
		},
		{
			name: "yaml shorthand",
			args: []string{"--yaml"},
			want: cliOptions{outputFormat: "yaml"},
		},
		{
			name: "profile long",
			args: []string{"--profile", "staging"},
			want: cliOptions{profileName: "staging"},
		},
		{
			name: "profile long equals",
			args: []string{"--profile=staging"},
			want: cliOptions{profileName: "staging"},
		},
		{
			name: "profile short",
			args: []string{"-p", "staging"},
			want: cliOptions{profileName: "staging"},
		},
		{
			name: "generate long",
			args: []string{"--generate", "curl"},
			want: cliOptions{generateFormat: "curl"},
		},
		{
			name: "generate short",
			args: []string{"-g", "go"},
			want: cliOptions{generateFormat: "go"},
		},
		{
			name: "generate output long",
			args: []string{"--generate-output", "out.go"},
			want: cliOptions{generateOutput: "out.go"},
		},
		{
			name: "generate output short",
			args: []string{"-O", "out.go"},
			want: cliOptions{generateOutput: "out.go"},
		},
		{
			name: "all options together",
			args: []string{"--limit", "5", "--output", "json", "--profile", "staging", "--generate", "curl", "--generate-output", "out.sh"},
			want: cliOptions{outputFormat: "json", profileName: "staging", generateFormat: "curl", generateOutput: "out.sh"},
		},
		{
			name: "--output wins over --json regardless of order",
			args: []string{"--json", "--output", "yaml"},
			want: cliOptions{outputFormat: "yaml"},
		},
		{
			name: "--output wins even when --yaml comes later",
			args: []string{"--output", "json", "--yaml"},
			want: cliOptions{outputFormat: "json"},
		},
		{
			name: "last format shorthand wins",
			args: []string{"--json", "--yaml"},
			want: cliOptions{outputFormat: "yaml"},
		},
		{
			name: "delimiter stops CLI option parsing",
			args: []string{"--output", "json", "--", "--profile", "evil", "--json"},
			want: cliOptions{outputFormat: "json"},
		},
		{
			name: "unknown request flags are ignored",
			args: []string{"--limit", "5", "--json"},
			want: cliOptions{outputFormat: "json"},
		},
		{
			name: "output without value falls through to shorthand",
			args: []string{"--output", "--json"},
			want: cliOptions{outputFormat: "json"},
		},
		{
			name: "short request flags are not mistaken for CLI options",
			args: []string{"-n", "5", "-o", "yaml"},
			want: cliOptions{outputFormat: "yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCLIOptions(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseCLIOptions(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

// TestGetProfileSelection covers profile resolution, the --profile override and
// the "profile not found" error which must list the available profiles.
func TestGetProfileSelection(t *testing.T) {
	h := &Handler{}
	appConfig := &config.AppConfig{
		Name:           "testapp",
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {Name: "default", BaseURL: "http://default.example"},
			"staging": {Name: "staging", BaseURL: "http://staging.example"},
		},
	}

	t.Run("default profile when name is empty", func(t *testing.T) {
		profile, err := h.getProfile(appConfig, "")
		if err != nil {
			t.Fatalf("getProfile() unexpected error: %v", err)
		}
		if profile.Name != "default" {
			t.Errorf("getProfile() = %q, want default", profile.Name)
		}
	})

	t.Run("explicit profile", func(t *testing.T) {
		profile, err := h.getProfile(appConfig, "staging")
		if err != nil {
			t.Fatalf("getProfile() unexpected error: %v", err)
		}
		if profile.Name != "staging" || profile.BaseURL != "http://staging.example" {
			t.Errorf("getProfile() = %+v, want staging profile", profile)
		}
	})

	t.Run("unknown profile lists available profiles", func(t *testing.T) {
		_, err := h.getProfile(appConfig, "missing")
		if err == nil {
			t.Fatal("getProfile() expected error, got nil")
		}
		for _, want := range []string{`profile "missing"`, "available profiles", "default", "staging"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("getProfile() error = %q, want it to contain %q", err.Error(), want)
			}
		}
	})

	t.Run("missing configured default profile is an error", func(t *testing.T) {
		broken := &config.AppConfig{
			Name:           "broken",
			DefaultProfile: "prod",
			Profiles: map[string]config.Profile{
				"default": {Name: "default"},
			},
		}
		_, err := h.getProfile(broken, "")
		if err == nil {
			t.Fatal("getProfile() expected error for missing default profile, got nil")
		}
		if !strings.Contains(err.Error(), `profile "prod"`) {
			t.Errorf("getProfile() error = %q, want it to name the missing profile", err.Error())
		}
	})

	t.Run("empty config does not panic", func(t *testing.T) {
		_, err := h.getProfile(&config.AppConfig{Name: "empty"}, "")
		if err == nil {
			t.Fatal("getProfile() expected error for empty config, got nil")
		}
	})
}

// TestApplyProfileToRequestPreservesQueryAndUserInfo verifies that profile
// headers and query parameters are layered onto a request without dropping
// existing query values or URL userinfo.
func TestApplyProfileToRequestPreservesQueryAndUserInfo(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://user:pass@example.com/pets?limit=5", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error: %v", err)
	}
	req.Header.Set("X-Existing", "keep")

	profile := &config.Profile{
		Name:        "staging",
		Headers:     map[string]string{"X-Profile": "staging"},
		QueryParams: map[string]string{"api-version": "v2"},
	}
	applyProfileToRequest(req, profile)

	if req.URL.User == nil {
		t.Fatal("applyProfileToRequest() dropped URL userinfo")
	}
	if got, _ := req.URL.User.Password(); req.URL.User.Username() != "user" || got != "pass" {
		t.Errorf("applyProfileToRequest() userinfo = %v, want user:pass", req.URL.User)
	}

	query := req.URL.Query()
	if got := query.Get("limit"); got != "5" {
		t.Errorf("existing query parameter limit = %q, want 5", got)
	}
	if got := query.Get("api-version"); got != "v2" {
		t.Errorf("profile query parameter api-version = %q, want v2", got)
	}

	if got := req.Header.Get("X-Existing"); got != "keep" {
		t.Errorf("existing header X-Existing = %q, want keep", got)
	}
	if got := req.Header.Get("X-Profile"); got != "staging" {
		t.Errorf("profile header X-Profile = %q, want staging", got)
	}
}

// TestApplyProfileToRequestNilInputs ensures the helper is defensive.
func TestApplyProfileToRequestNilInputs(t *testing.T) {
	applyProfileToRequest(nil, &config.Profile{})
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	applyProfileToRequest(req, nil)
	if req.URL == nil {
		t.Fatal("request URL unexpectedly nil")
	}
}

// TestProfileQueryParamsKeepExistingQueryValue documents the override semantics:
// other query keys are preserved, while a key explicitly configured in the
// profile takes precedence.
func TestProfileQueryParamsKeepExistingQueryValue(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/pets?limit=5&page=1", nil)
	profile := &config.Profile{QueryParams: map[string]string{"limit": "10"}}
	applyProfileToRequest(req, profile)

	query := req.URL.Query()
	if got := query.Get("limit"); got != "10" {
		t.Errorf("profile query limit = %q, want 10 (profile overrides)", got)
	}
	if got := query.Get("page"); got != "1" {
		t.Errorf("existing query page = %q, want 1 (preserved)", got)
	}
}

// TestParseCLIOptionsDelimitedRequestParameters ensures `--` protected request
// parameters are never interpreted as CLI options.
func TestParseCLIOptionsDelimitedRequestParameters(t *testing.T) {
	parsed := parseCLIOptions([]string{"--profile", "staging", "--", "--output", "json", "--json"})
	if parsed.profileName != "staging" {
		t.Errorf("profileName = %q, want staging", parsed.profileName)
	}
	if parsed.outputFormat != "" {
		t.Errorf("outputFormat = %q, want empty (flags after -- are request parameters)", parsed.outputFormat)
	}
}
