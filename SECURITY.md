# Security Policy

## Supported Versions

OpenBridge is pre-1.0 and ships fixes on the latest minor release only.

| Version | Supported |
| ------- | --------- |
| latest `0.x` release | :white_check_mark: |
| older `0.x` releases | :x: |

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Report privately via one of the following:

1. **GitHub Security Advisories** (preferred):
   <https://github.com/nomagicln/open-bridge/security/advisories/new>
2. Email the maintainers at <security@openbridge.dev> if the advisory form is
   not available.

Please include:

- affected version (`ob --version`) and operating system;
- a description of the vulnerability and its impact;
- reproduction steps or a proof-of-concept (a minimal OpenAPI specification and
  the command you ran, when relevant);
- any suggested fix or mitigation.

## What to Expect

| Stage | Target |
| ----- | ------ |
| Acknowledgement of the report | within 3 business days |
| Initial assessment and severity triage | within 7 business days |
| Fix or mitigation plan for confirmed issues | within 30 days |
| Public disclosure | coordinated with the reporter after a fix is released |

Credit will be given in the release notes unless you prefer to stay anonymous.

## Scope

In scope:

- the `ob` CLI and its MCP server mode;
- credential handling (keyring storage, file backend, secret masking in code
  generation and error output);
- YAML/OpenAPI specification parsing paths that can crash or exhaust resources;
- generated shim scripts and install/uninstall flows.

Out of scope:

- vulnerabilities in third-party dependencies that already have an upstream
  advisory (they are tracked by `govulncheck` in CI and Dependabot);
- issues that require an attacker to already control the user's machine or
  shell environment;
- denial of service caused by intentionally malicious OpenAPI specifications
  supplied by the same user who runs the command.

## Security Practices

- Dependencies are monitored with `govulncheck` in CI and Dependabot updates.
- Static analysis runs on every pull request (golangci-lint, CodeQL).
- Credentials are stored in the system keyring; file-backend usage requires an
  explicit `OPENBRIDGE_KEYRING_PASSWORD`.
