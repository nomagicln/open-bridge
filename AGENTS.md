# OpenBridge AI Agent Guidelines

## 1. Constitution (Non-Negotiable)

These rules are absolute. Any deviation requires explicit human approval.

### 1.1 The Single Source of Truth

* **Linting:** `.golangci.yml` is the supreme law. Never modify it to silence errors.
* **Overrides:** Use `//nolint` directives only as a last resort and **MUST** include a comment explaining why (e.g., `//nolint:gosec // Safe because...`).

### 1.2 Zero-Inference Policy

* Do not guess business logic. If a requirement is ambiguous, ask the user for clarification before writing code.
* Do not invent external dependencies. Use the standard library whenever possible.

---

## 2. Test-Driven Development (The Protocol)

**Principle:** Tests define the requirement. Implementation is merely a solution to pass the test.

The Agent **MUST** follow this strict 3-step sequence for any new functionality. **Do not attempt to generate the full solution in one pass.**

### Step 1: Interface Design (Skeleton)

*Goal: Define the contract and ensure compilation.*

1. Analyze requirements.
2. Define exported types, interfaces, and function signatures in the source file (`foo.go`).
3. **DO NOT** implement logic. Use placeholders to ensure the code compiles.

```go
func CalculateMetric(data []int) (int, error) {
    panic("unimplemented") // Placeholder
}

```

### Step 2: Write The Test (The "Red" State)

*Goal: Codify behavior and edge cases.*

1. Create the test file (`foo_test.go`).
2. Write comprehensive test cases covering happy paths and error scenarios based **only** on the signatures from Step 1.
3. **Constraint:** The tests must compile, but they should fail (panic) when run.

### Step 3: Implementation (The "Green" State)

*Goal: Pass the tests with minimal code.*

1. Implement the business logic in `foo.go` to satisfy the tests.
2. Replace `panic("unimplemented")` with actual logic.
3. **YAGNI:** Do not add public functions or logic not covered by the tests.

---

## 3. Test Architecture & Organization

### 3.1 File Naming Conventions (Strict)

| Test Type | File Name | Build Tag Requirement | Description |
| --- | --- | --- | --- |
| **Unit** | `foo_test.go` | None | Tests logic within `foo.go` in isolation. |
| **Integration** | `foo_integration_test.go` | `//go:build integration` | Tests involving DB, HTTP, or FS. |
| **Property** | `foo_property_test.go` | None | Randomized input testing with `gopter`. |
| **Helpers** | `test_helpers.go` | None | Shared test utilities within the package. |

**Rules:**

1. **Strict Name Matching:** `foo.go`  `foo_test.go`. Never use mismatched names like `foo_service_test.go`.
2. **No Orphan Tests:** Every test file must correspond to an existing source file.

### 3.2 Property Testing Strategy

You **MUST** use property-based testing (using `gopter` or similar) for:

* Parsers and formatters.
* Serialization/Deserialization logic.
* Complex algorithms with invariant properties (e.g., `Decode(Encode(x)) == x`).
* **Naming:** All property test functions must start with `TestProperty`.

### 3.3 Integration Test Isolation

All files ending in `_integration_test.go` **MUST** include the build tag at the very top:

```go
//go:build integration

package mypackage_test

```

---

## 4. Coding Standards

### 4.1 Error Handling

* **Wrapping:** Always wrap errors when passing them up the stack to preserve context.

```go
// ✅ Good
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}
// ❌ Bad
if err != nil {
    return err
}

```

* **Sentinel Errors:** Define strict sentinel errors (e.g., `ErrConfigNotFound`) for state checks in the `var` block of the package.

### 4.2 Mocking & Dependency Injection

* **Interfaces:** Always define dependencies as interfaces, not concrete structs.
* **Generation:** If a `Generate Mocks` task is required, prefer standard tools (like `mockery`) over hand-written mocks unless the interface is trivial.

---

## 5. Operational Guidelines

### 5.1 Before Committing (The Checklist)

Run the following commands. All must pass:

1. **Format & Lint:**

    ```bash
    make fmt-fix && make lint-fix
    ```

2. **Unit Tests:**

    ```bash
    go test -v -short ./...
    ```

3. **Integration Tests (Optional but Recommended):**

    ```bash
    go test -v -tags=integration ./...
    ```

4. **Documentation Sync Check:**

    * Review your changes and ensure all modifications are reflected in relevant documentation (e.g., README files, API docs, code comments, or guides).

    * If the change affects user-facing functionality, verify that documentation is updated accordingly.

    * When in doubt, ask whether the change requires documentation updates.

### 5.2 Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):
`type(scope): description`

* **Types:** `feat`, `fix`, `refactor`, `test`, `chore`, `docs`.
* **Scopes:** `spec`, `config`, `cli`, `tui`, `mcp`.
* **Sign-off:** All commits **MUST** include DCO sign-off (`-s` flag).

**Example:**

```bash
git commit -m "feat(cli): add code generation support" -s
```

All commits without sign-off will be rejected by the DCO check.

---

## 6. How to Read This Document (For AI)

1. **Context:** You are an expert Golang engineer working on the OpenBridge project.
2. **Trigger:** When asked to "Implement feature X", you automatically initiate the **Step 1 (Interface Design)** of the TDD Protocol defined in Section 2.
3. **Constraint:** You stop after Step 2 to wait for user confirmation before proceeding to Implementation (Step 3).
