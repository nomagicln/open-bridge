# OpenBridge AI Agent Guidelines

## Constitution (Non-Negotiable)

### 1. `.golangci.yml` is the Single Source of Truth

- **Never modify** the `.golangci.yml` file
- Use `//nolint` directives only when strictly necessary to preserve readability or consistency

### 2. Test-Driven Development

- Workflow: **Requirements → Tests → Implementation**
- Never modify tests to make them pass (unless the test logic itself is flawed)

### 3. Test Requirements

- Unit test pass rate: **100%**
- Code coverage: **≥ 80%**

### 4. Test Organization

#### File Naming Conventions

| Test Type | File Name | Description |
|-----------|-----------|-------------|
| Unit | `foo_test.go` | Corresponds to `foo.go` |
| Integration | `foo_integration_test.go` | Tests involving external dependencies (HTTP, filesystem, etc.) |
| Property | `foo_property_test.go` | Randomized input testing with `gopter` |
| Helpers | `test_helpers.go` | Shared test utilities within a package |

#### File Naming Rules (Mandatory)

1. **Strict Name Matching**: Test file names **MUST** exactly match their source file names
   - `errors.go` → `errors_test.go`, `errors_integration_test.go`, `errors_property_test.go`
   - `install.go` → `install_test.go`, `install_integration_test.go`
   - ❌ `error_test.go` for `errors.go` is **WRONG**
   - ❌ `shim_integration_test.go` for `install.go` is **WRONG**

2. **No Orphan Test Files**: Every test file must have a corresponding source file
   - ❌ `spec_source_test.go` is invalid if `spec_source.go` does not exist
   - ✅ Move such tests to the correct file (e.g., merge into `install_test.go`)

3. **Function Placement**: Test functions must be placed in the test file corresponding to the source file where the function under test is defined

#### Principles

- **Colocation**: All tests (unit/integration/property) must correspond to their source file for maintainability
- **Extract helpers**: When multiple test files share common logic, extract it to `test_helpers.go`
- **Property tests**: Core algorithms, parsers, and mapping logic should have property tests to verify invariants
- **Use testify**: Use `assert` for assertions; use `require` when failure should halt the test

#### Property Test Naming Convention

All property test functions **MUST** use the `TestProperty` prefix:

```go
// Good
func TestPropertyConfigPersistenceRoundTrip(t *testing.T) { ... }
func TestPropertyCredentialRoundTrip(t *testing.T) { ... }

// Bad - missing TestProperty prefix
func TestConfigRoundTrip(t *testing.T) { ... }
func TestRoundTripProperty(t *testing.T) { ... }
```

This naming convention enables:

- Running property tests separately: `make test-property`
- Skipping property tests in short mode: `go test -short ./...`

#### testify Usage

```go
import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestExample(t *testing.T) {
    // require: halts on failure, use for preconditions
    result, err := DoSomething()
    require.NoError(t, err)
    require.NotNil(t, result)

    // assert: continues on failure, use for verifying results
    assert.Equal(t, expected, result.Value)
    assert.Contains(t, result.List, item)
}
```

---

## Operational Guidelines

### 1. Before Writing Code

Read `.golangci.yml` to understand the coding standards.

### 2. Before Writing Tests

Verify the test file name matches the source file:

```bash
# Example: If you're testing functions in install.go
# ✅ Correct: install_test.go, install_integration_test.go
# ❌ Wrong: shim_test.go, spec_source_test.go

# List source files to confirm naming
ls pkg/<package>/*.go | grep -v _test.go
```

### 3. Before Committing

Run the following commands and ensure all checks pass:

```bash
make fmt-fix && make lint-fix && make test
```

Additionally, verify test file naming compliance:

```bash
# Ensure no orphan test files exist (test files without corresponding source files)
# For each *_test.go file, the base name should match a *.go source file
```

### 4. Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

[body]

[footer]
```

**Type**: `feat` | `fix` | `refactor` | `docs` | `test` | `chore`

**Scope**: `spec` | `config` | `credential` | `semantic` | `request` | `cli` | `mcp` | `tui`

### 5. Code Readability

- Add comments to explain **why**, not what
- Complex logic must include comments describing the intent
