# Contributing to OpenBridge

Thanks for taking the time to contribute! This guide explains how to set up your environment, follow the project’s development workflow, and submit changes.

## Code of Conduct

Please read and follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Reading Order

Start with the [README](README.md), then read the [current architecture](docs/reference/architecture.md) for module boundaries and data flow, [AGENTS.md](AGENTS.md) for mandatory engineering rules, and this guide for local checks. Read the [upgrade guide](docs/guide/upgrading.md) before changing a CLI, MCP, configuration, or public Go contract; historical reports are evidence rather than the current API reference.

## Prerequisites

- Go 1.26 or later (CI tests Go 1.27 on Linux, macOS, and Windows, plus Go 1.26 on Linux)
- Make (optional, but recommended)
- Tools (optional but recommended): `golangci-lint`, `goimports`

Check tool availability:

```bash
make check-tools
```

Install dependencies:

```bash
make deps
```

## Development Workflow

1. Fork the repository and create a feature branch.
2. Make small, focused changes.
3. Follow the TDD protocol and code style rules below.
4. Run checks locally.
5. Open a pull request.

## TDD Protocol (Required)

New functionality follows a strict 3-step TDD flow (see [AGENTS.md](AGENTS.md)):

1. **Interface Design (Skeleton)**: define exported types and function signatures only. Use placeholders like `panic("unimplemented")` and verify compilation.
2. **Write Tests (Red)**: add tests that compile but fail. Stop here and wait for explicit user confirmation before Step 3; historical reports do not waive this checkpoint.
3. **Implementation (Green)**: implement the minimal logic to pass tests.

For existing defects, add a failing regression test before correcting the implementation. Do not insert placeholder panics into working code.

## Testing

Run the full test suite:

```bash
make test
```

Quick tests (skip property tests):

```bash
make test-short
```

Coverage report:

```bash
make test-coverage
```

Integration tests (when needed):

```bash
go test -v -tags=integration ./...
```

### Test File Naming (Strict)

| Test Type | File Name | Build Tag | Notes |
| --- | --- | --- | --- |
| Unit | `foo_test.go` | None | Tests `foo.go` in isolation |
| Integration | `foo_integration_test.go` | `//go:build integration` | DB/HTTP/FS tests |
| Property | `foo_property_test.go` | None | Must use `gopter` |
| Helpers | `test_helpers_test.go` | None | Package-local helpers, excluded from production builds |

Unit and property tests must align with the real source file they exercise. Test helpers and test-only cross-package integration packages such as `internal/integration` are exempt from paired-source naming; do not create artificial production files for them. Integration build tags still apply.

Shared test-only packages such as `internal/proptest` and `internal/testutil` may retain `.go` files so tests in other packages can import them. Production packages must not import these helpers. `testing`, `testify`, and `gopter` are valid test dependencies in `go.mod`, but `make production-deps` rejects them in `go list -deps ./cmd/ob`.

Property tests are **required** for parsers, serializers, and complex algorithms.

## Code Style and Linting

- `.golangci.yml` is the single source of linting truth. Do not modify it to silence errors.
- Use `gofmt` and `goimports`.
- Prefer standard library packages over new dependencies.

Run formatting and linting:

```bash
make fmt-fix
make lint-fix
```

The Makefile pins `golangci-lint` to `v2.15.0`. `make lint-version` checks the selected binary's version and requires it to have been built with at least the Go major/minor version currently used by the project. `make lint` and `make lint-fix` run this preflight; `GOLANGCI_LINT` selects the executable without changing `PATH`.

If your binary uses an older configuration format or was built with an older Go version, build the pinned tool with your current Go toolchain in a separate directory, then select it explicitly:

```bash
make install-lint GOBIN="$HOME/.local/share/open-bridge/lint-bin"
make lint-version GOLANGCI_LINT="$HOME/.local/share/open-bridge/lint-bin/golangci-lint"
make lint-fix GOLANGCI_LINT="$HOME/.local/share/open-bridge/lint-bin/golangci-lint"
```

Use the `.exe` executable path on Windows. Make targets use a POSIX `sh` environment (for example, Git Bash on Windows). Keep `.golangci.yml` unchanged when resolving tool version issues.

Run the local quality gate and check module tidiness before opening a pull request:

```bash
make check
go mod tidy -diff
```

`make check` verifies production imports, formatting, vet with both build tags, the selected linter, and short tests. Run `make production-deps` on its own for a quick production/test dependency boundary check. CI runs this check on every OS/version test entry, integration tests on both Linux Go versions, and module tidiness plus coverage upload only on Linux Go 1.27.

### Error Handling

- Wrap errors with context using `%w`.
- Define sentinel errors for state checks (for example `ErrConfigNotFound`).

## Commit Messages (Conventional Commits)

Format: `type(scope): description`

Types: `feat`, `fix`, `refactor`, `test`, `chore`, `docs`

Scopes: `app`, `cli`, `codegen`, `completion`, `config`, `credential`, `install`, `mcp`, `request`, `router`, `semantic`, `spec`, `tui`; use `ci`, `deps`, or `docs` for repository infrastructure.

Example: `feat(config): add profile switching support`

## DCO Sign-off (Required)

All commits must include a `Signed-off-by` line. Use:

```bash
git commit -s
```

By signing off, you agree to the [Developer Certificate of Origin](https://developercertificate.org/).

## Pull Requests

Before opening a PR, ensure:

- Tests pass: `make test`; run integration tests for affected boundaries
- Formatting and linting are clean: `make fmt-fix && make lint-fix` (select a compatible `GOLANGCI_LINT` if needed)
- Local quality gate passes: `make check`, including `make production-deps`
- Module files are tidy: `go mod tidy -diff`
- Current architecture and upgrade documentation updated when their contracts change

Use the PR template and reference any related issues.

## Reporting Bugs / Requesting Features

Use the GitHub issue templates:

- Bug reports: [.github/ISSUE_TEMPLATE/bug_report.md](.github/ISSUE_TEMPLATE/bug_report.md)
- Feature requests: [.github/ISSUE_TEMPLATE/feature_request.md](.github/ISSUE_TEMPLATE/feature_request.md)
