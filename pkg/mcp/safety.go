package mcp

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/pkg/config"
)

// confirmArgumentName is the optional boolean argument an AI client can pass to
// acknowledge an operation listed in SafetyConfig.RequireConfirm.
const confirmArgumentName = "_confirm"

// confirmErrorMessage is returned when an operation listed in
// SafetyConfig.RequireConfirm is invoked without an explicit confirmation.
const confirmErrorMessage = "operation requires confirmation; re-invoke with " + confirmArgumentName + "=true"

// safetyGuard enforces the invocation-time parts of config.SafetyConfig.
//
// Semantics (see config.SafetyConfig for the YAML keys):
//   - ReadOnlyMode / AllowedOperations / DeniedOperations are enforced when tools
//     are built (visibility), and keep their existing behavior.
//   - RequireConfirm lists HTTP methods whose operations must be explicitly
//     confirmed through the "_confirm=true" tool argument on every call; the
//     confirmation is never remembered.
//   - MaxRequestsPerMinute limits API invocations per process using a fixed
//     one-minute window; over-limit calls are rejected with the remaining wait.
//   - DangerousOperationPatterns are regular expressions matched against
//     "METHOD path" and the tool ID; a match rejects the call (confirmation
//     cannot override it).
//
// The guard is safe for concurrent use: the rate limiter is mutex-protected and
// the compiled patterns are immutable after construction.
type safetyGuard struct {
	requireConfirm map[string]struct{}
	dangerous      []compiledDangerousPattern
	limiter        *requestRateLimiter
}

// compiledDangerousPattern keeps the original pattern source for error messages.
type compiledDangerousPattern struct {
	source string
	re     *regexp.Regexp
}

// newSafetyGuard compiles the safety configuration and returns a guard that
// enforces it at invocation time.
func newSafetyGuard(cfg *config.SafetyConfig) (*safetyGuard, error) {
	guard := &safetyGuard{requireConfirm: make(map[string]struct{})}
	if cfg == nil {
		return guard, nil
	}

	for _, method := range cfg.RequireConfirm {
		normalized := strings.ToUpper(strings.TrimSpace(method))
		if normalized != "" {
			guard.requireConfirm[normalized] = struct{}{}
		}
	}

	for _, pattern := range cfg.DangerousOperationPatterns {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid dangerous_operation_patterns entry %q: %w", pattern, err)
		}
		guard.dangerous = append(guard.dangerous, compiledDangerousPattern{source: pattern, re: compiled})
	}

	guard.limiter = newRequestRateLimiter(cfg.MaxRequestsPerMinute)

	return guard, nil
}

// confirmationRequired reports whether invocations with method need confirmation.
func (g *safetyGuard) confirmationRequired(method string) bool {
	if g == nil {
		return false
	}
	_, ok := g.requireConfirm[strings.ToUpper(method)]
	return ok
}

// dangerousMatch reports the first dangerous pattern matching "METHOD path" or
// any of the provided tool IDs.
func (g *safetyGuard) dangerousMatch(method, path string, toolIDs ...string) (string, bool) {
	if g == nil || len(g.dangerous) == 0 {
		return "", false
	}

	candidates := make([]string, 0, len(toolIDs)+1)
	candidates = append(candidates, strings.ToUpper(method)+" "+path)
	candidates = append(candidates, toolIDs...)

	for _, pattern := range g.dangerous {
		if slices.ContainsFunc(candidates, pattern.re.MatchString) {
			return pattern.source, true
		}
	}

	return "", false
}

// allowRequest consumes one request slot; the second result reports whether the
// request is allowed. When denied, the first result is the remaining wait time.
func (g *safetyGuard) allowRequest() (time.Duration, bool) {
	if g == nil || g.limiter == nil {
		return 0, true
	}
	return g.limiter.Allow()
}

// checkInvocation enforces invocation-time safety checks. It returns a non-nil
// isError result when the invocation must be rejected, and nil when it may run.
// Checks are applied in this order: dangerous patterns, confirmation, rate limit.
func (g *safetyGuard) checkInvocation(toolID, method, path string, confirmed bool) *mcp.CallToolResult {
	if g == nil {
		return nil
	}

	if pattern, matched := g.dangerousMatch(method, path, toolID); matched {
		return errorResult("operation blocked by dangerous_operation_patterns: pattern %q matches %s %s (tool %q)",
			pattern, method, path, toolID)
	}

	if g.confirmationRequired(method) && !confirmed {
		return errorResult("%s", confirmErrorMessage)
	}

	if remaining, allowed := g.allowRequest(); !allowed {
		return errorResult("rate limit exceeded: max_requests_per_minute=%d permitted; retry after %d seconds",
			g.limiter.limit, retryAfterSeconds(remaining))
	}

	return nil
}

// requestRateLimiter implements a mutex-protected fixed-window request counter.
type requestRateLimiter struct {
	mu          sync.Mutex
	limit       int
	window      time.Duration
	windowStart time.Time
	count       int
	now         func() time.Time
}

// newRequestRateLimiter creates a fixed one-minute window limiter, or nil when
// limit is not positive (disabled).
func newRequestRateLimiter(limit int) *requestRateLimiter {
	if limit <= 0 {
		return nil
	}
	return &requestRateLimiter{
		limit:  limit,
		window: time.Minute,
		now:    time.Now,
	}
}

// Allow reports whether a request may be made now and, when it may not, how long
// the caller should wait before the current window resets.
func (l *requestRateLimiter) Allow() (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if l.windowStart.IsZero() || !now.Before(l.windowStart.Add(l.window)) {
		l.windowStart = now
		l.count = 0
	}

	if l.count >= l.limit {
		return l.windowStart.Add(l.window).Sub(now), false
	}
	l.count++

	return 0, true
}

// retryAfterSeconds converts a wait duration to whole seconds (minimum one).
func retryAfterSeconds(wait time.Duration) int {
	if wait <= 0 {
		return 1
	}
	seconds := int((wait + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

// confirmRequested reports whether the tool arguments carry an explicit
// confirmation (accepts boolean true or the string "true").
func confirmRequested(arguments map[string]any) bool {
	value, ok := arguments[confirmArgumentName]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return false
	}
}

// stripConfirmArgument removes the invocation-only confirmation flag so it is
// never forwarded to the API as query/body/header data.
func stripConfirmArgument(arguments map[string]any) {
	delete(arguments, confirmArgumentName)
}

// ValidateSafetyConfig validates invocation-time safety configuration so that
// MCP startup can fail fast on invalid configuration (e.g. bad regexes).
func ValidateSafetyConfig(cfg *config.SafetyConfig) error {
	_, err := newSafetyGuard(cfg)
	return err
}
