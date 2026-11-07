# CLI Reference

## ob Commands

Global commands for managing OpenBridge applications.

| Command | Description |
|---------|-------------|
| `ob install <name> --spec <path>` | Install an API as a CLI application |
| `ob uninstall <name>` | Remove an installed application |
| `ob list` | List all installed applications |
| `ob run <name> [args...]` | Run commands for an installed application |
| `ob completion [bash\|zsh\|fish]` | Generate shell completion script |
| `ob --version` | Show version information |
| `ob help` | Show help |

## App Commands

Commands available for installed applications.

| Pattern | Example |
|---------|---------|
| `<app> <resource> list` | `myapi users list` |
| `<app> <resource> get --id <id>` | `myapi user get --id 123` |
| `<app> <resource> create [flags]` | `myapi user create --name "John"` |
| `<app> <resource> update [flags]` | `myapi user update --id 123 --name "Jane"` |
| `<app> <resource> delete --id <id>` | `myapi user delete --id 123` |

## Output Formats

Control the output format using flags:

```bash
# YAML output (default)
myapi users list

# JSON output
myapi users list --json

# Explicit format selection
myapi users list --output json
myapi users list --yaml
```

## Caching

OpenBridge stores derived data under the configuration directory:

* `<config-dir>/spec-cache` — remote OpenAPI specifications, revalidated with `ETag` / `Last-Modified`
  when stale and used as an offline fallback when the remote source is unreachable.
* `<config-dir>/index-cache` — derived help/completion indexes. Local specs are keyed by absolute path,
  size and modification time; remote specs are keyed by URL and content hash. Indexes are versioned and
  rebuilt automatically when the source or the semantic rules change.

Deleting either directory is always safe; the data is rebuilt on demand.

## Profile Configuration

Each profile in `<config-dir>/apps/<app>.yaml` can tune HTTP behavior used by the CLI:

```yaml
profiles:
  prod:
    base_url: https://api.example.com
    timeout: 30s
    tls:
      ca_file: /etc/ssl/private-ca.pem   # custom CA bundle
      cert_file: /etc/ssl/client.pem     # optional mTLS client certificate
      key_file: /etc/ssl/client-key.pem
      server_name: api.example.com       # optional SNI override
      min_version: "1.2"                 # "1.2" or "1.3"
      insecure_skip_verify: false
    retry:
      max_retries: 3
      initial_delay: 200ms
      max_delay: 2s
      retryable_status_codes: [429, 503]
```

* `timeout` defaults to `30s`. `tls` and `retry` settings are applied to CLI requests.
* Only idempotent methods (`GET`, `HEAD`, `PUT`, `DELETE`, `OPTIONS`) are retried; request bodies are replayed automatically.

## MCP Safety Controls

`safety` settings are enforced when MCP tools are invoked:

```yaml
profiles:
  default:
    safety:
      read_only_mode: true
      allowed_operations: [listPets]
      denied_operations: [deletePet]
      require_confirm: [POST, PUT, DELETE]
      max_requests_per_minute: 30
      dangerous_operation_patterns:
        - "POST /admin"
        - "DELETE /pets"
```

* `require_confirm` methods are rejected until the tool call includes `_confirm: true`; confirmation is never remembered.
* `max_requests_per_minute` is enforced per process; over-limit calls fail with the remaining wait time.
* `dangerous_operation_patterns` are regular expressions matched against `METHOD path` and the tool ID; matches are rejected even with confirmation.
* `read_only_mode`, `allowed_operations` and `denied_operations` control which tools are exposed.

## OpenAPI Extensions

Customize CLI behavior using `x-cli-*` extensions in your OpenAPI spec:

```yaml
paths:
  /server/reboot:
    post:
      x-cli-verb: trigger      # Override default verb mapping
      x-cli-resource: server   # Override resource name
```
