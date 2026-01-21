# Copilot Instructions

## Prerequisites

Install formatting and linting tools before running `make fmt-fix && make lint-fix`:

```bash
go install golang.org/x/tools/cmd/goimports@latest
make install-lint
```

Ensure `$(go env GOPATH)/bin` is in your `PATH` so `goimports` and `golangci-lint` are discoverable.
