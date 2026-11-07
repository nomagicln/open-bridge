package cli

import "fmt"

// UnknownResourceError reports that a command referenced a resource that is not
// exposed by the application's OpenAPI specification.
type UnknownResourceError struct {
	Resource string
}

// Error implements error. The text matches the pre-typed-error wording.
func (e *UnknownResourceError) Error() string {
	return fmt.Sprintf("unknown resource: %s", e.Resource)
}

// UnknownVerbError reports an unsupported verb for a known resource.
type UnknownVerbError struct {
	Resource string
	Verb     string
}

// Error implements error. The text matches the pre-typed-error wording.
func (e *UnknownVerbError) Error() string {
	return fmt.Sprintf("unknown verb '%s' for resource '%s'", e.Verb, e.Resource)
}

// SpecLoadError reports a failure to load or parse an application's OpenAPI
// specification. Source identifies where the spec was expected to come from.
type SpecLoadError struct {
	Source string
	Err    error
}

// Error implements error. The text matches the pre-typed-error wording
// ("failed to load spec: <cause>").
func (e *SpecLoadError) Error() string {
	if e.Err == nil {
		return "failed to load spec"
	}
	return fmt.Sprintf("failed to load spec: %v", e.Err)
}

// Unwrap exposes the underlying spec loading error.
func (e *SpecLoadError) Unwrap() error {
	return e.Err
}

// displayError carries a pre-rendered user-facing message while preserving the
// underlying typed error for errors.As/errors.Is introspection.
type displayError struct {
	message string
	err     error
}

// Error returns the pre-rendered user-facing message.
func (e *displayError) Error() string {
	return e.message
}

// Unwrap exposes the typed error.
func (e *displayError) Unwrap() error {
	return e.err
}
