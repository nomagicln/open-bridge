# Contributing to OpenBridge

Thanks for taking the time to contribute! This guide explains how to set up your environment, follow the project’s development workflow, and submit changes.

## Code of Conduct

Please read and follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Prerequisites

- Go 1.25.5 or later
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

This project follows a strict 3‑step TDD flow (see [AGENTS.md](AGENTS.md)):

1. **Interface Design (Skeleton)**: define exported types and function signatures only. Use placeholders like `panic("unimplemented")`.
2. **Write Tests (Red)**: add tests that compile but fail.
3. **Implementation (Green)**: implement the minimal logic to pass tests.

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
| Helpers | `test_helpers.go` | None | Shared helpers |

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

### Error Handling

- Wrap errors with context using `%w`.
- Define sentinel errors for state checks (for example `ErrConfigNotFound`).

## Commit Messages (Conventional Commits)

Format: `type(scope): description`

Types: `feat`, `fix`, `refactor`, `test`, `chore`, `docs`

Scopes: `spec`, `config`, `cli`, `tui`, `mcp`

Example: `feat(config): add profile switching support`

## DCO Sign-off (Required)

All commits must include a `Signed-off-by` line. Use:

```bash
git commit -s
```

By signing off, you agree to the [Developer Certificate of Origin](https://developercertificate.org/).

## Pull Requests

Before opening a PR, ensure:

- Tests pass: `make test`
- Formatting and linting are clean: `make fmt-fix && make lint-fix`
- Documentation updated (if applicable)

Use the PR template and reference any related issues.

## Reporting Bugs / Requesting Features

Use the GitHub issue templates:

- Bug reports: [.github/ISSUE_TEMPLATE/bug_report.md](.github/ISSUE_TEMPLATE/bug_report.md)
- Feature requests: [.github/ISSUE_TEMPLATE/feature_request.md](.github/ISSUE_TEMPLATE/feature_request.md)
