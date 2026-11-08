# CLI Reference

This reference follows `main`. **Unreleased** behavior is identified explicitly; `@latest` currently installs **v0.1.3**. Check the [upgrade guide](/guide/upgrading) before updating an existing integration.

## ob commands

| Command | Description |
| --- | --- |
| `ob install <name> --spec <path-or-url>` | Install an API as a CLI application |
| `ob uninstall <name> [--remove-shim=false]` | Remove app configuration and, by default, its shim |
| `ob list [--details]` | List installed applications; `--details` / `-d` shows more information |
| `ob info <name> [-o text\|json\|yaml]` | Inspect app configuration; default is `text` |
| `ob run <name> [args...]` | Run an installed app |
| `ob <name> [args...]` | Run an installed app directly |
| `ob completion [bash\|zsh\|fish]` | Generate shell completion |
| `ob --version` | Show build version information |
| `ob --help` | Show management command help |

`ob info -o yaml/json` is configuration inspection. It masks selected sensitive headers and is not the removed profile export format or a credential backup. Review any output before sharing it.

### Installation options

Interactive installation is **off by default**. Supply `--spec`, or explicitly opt into the wizard:

```bash
ob install myapi --spec ./openapi.yaml
ob install myapi --spec https://api.example.com/openapi.json --base-url https://api.example.com
ob install myapi -i
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--spec`, `-s` | empty | Local OpenAPI path or remote URL; required unless interactive |
| `--base-url` | detected from spec | Override the API request base URL |
| `--description` | empty | Application description |
| `--auth` | empty | Authentication type: `none`, `bearer`, `api_key`, `basic` |
| `--shim` | `true` | Create a command shortcut; use `--shim=false` to omit it |
| `--force`, `-f` | `false` | Overwrite existing app configuration |
| `--interactive`, `-i` | `false` | Launch the interactive installation wizard |

`--auth` selects the strategy; it does not accept a secret. Use the wizard/credential backend to configure credentials. Record the shim path printed by installation and add its directory to `PATH`, or use either `ob` invocation form without a shim. Use `--force` only when overwriting existing configuration is intended.

## App commands

The pattern is `<app> <resource> <verb> [flags]`. Resources and parameter names come from the installed spec; the examples below assume it has the corresponding user operations.

```bash
ob run myapi --help
ob myapi users --help
ob myapi users list
ob myapi users get --user_id 123
# With an installed shim on PATH:
myapi users list
```

URL subresources are flattened into compound resource names. For the repository Todo example, the current mapper derives:

| API operation | App command |
| --- | --- |
| `POST /todos` | `todo todos create --title "Task"` |
| `POST /todos/batch` | `todo todos-batch batch --items '[{"title":"Task"}]'` |
| `DELETE /todos/batch` | `todo todos-batch delete --ids '[1,2]'` |
| `GET /todos/stats` | `todo todos-stats get` |

Confirm names with app help. These Todo examples are derived from source; this iteration's runtime smoke status is tracked in the [architecture audit](/maintenance/architecture-audit).

### Runtime options

| App option | Meaning |
| --- | --- |
| `--profile`, `-p` | Select a configured profile |
| `--output`, `-o` | API response format: `json` or `yaml` (default `yaml`) |
| `--json`, `--yaml` | Select the API response format with a shorthand |
| `--generate`, `-g` | Generate a request snippet instead of sending the API request |
| `--generate-output`, `-O` | Write generated code to a file; default is stdout |
| `--mcp` | Start the app as an MCP server |
| `--transport` | MCP transport: `stdio` (default) or `sse` |
| `--port` | SSE port (default `8080`) |

Value options support `--name value` and `--name=value`; CLI profile/output/generation short options also support their documented separate-value forms. Keep runtime flags before the first `--`.

The **Unreleased** common runtime path for direct, `run`, and shim entrypoints follows these rules:

- Explicit root `--profile` / `--output` / `--mcp` values provide defaults for the app. Explicit app options override those defaults.
- Repeated values in the same scope use the last one. Within app output selection, explicit `--output` takes priority over `--json`/`--yaml` regardless of order; without `--output`, the last shorthand wins.
- The first `--` ends runtime option parsing. Options after it are request parameters and cannot start MCP or select a runtime profile/output.
- Missing required option values produce an error.

For example, this Unreleased invocation selects `prod` and YAML:

```bash
ob --profile dev --output json run myapi users list --profile prod --output yaml
```

### Request arguments

Without a separator, request flags are classified using the operation schema:

```bash
ob myapi users get --user_id 123
```

After `--`, use explicit location prefixes (`--path:`, `--query:`, `--header:`, `--body:`):

```bash
ob myapi users get --output json -- --path:user_id=123 --query:expand=details
```

Unreleased effective-parameter handling includes inherited path-level parameters, with operation-level overrides keyed by `(in, name)`. Raw JSON bodies are validated as the actual body before the API request is sent. See [upgrading](/guide/upgrading#unreleased-runtime-behavior-changes) for compatibility effects.

## Output and code generation

API responses default to YAML. Use `--json`, `--yaml`, or `--output json|yaml`; API response table formatting is not provided. Management commands such as `ob list` and `ob info` have their own display formats.

```bash
ob myapi users list --json
ob myapi users list --output yaml
ob myapi users list --generate curl
ob myapi users list --generate nodejs --generate-output request.js
ob myapi users list -g go -O request.go
ob myapi users list --generate python
```

Supported generation formats are **`curl`**, **`nodejs`**, **`go`**, and **`python`**. Generation does not execute the API request. Set profile `protect_sensitive_info: true` to replace credentials with placeholders in generated code; protection is not enabled by default. Inspect generated output before committing or sharing it.

## MCP transports and safety

```bash
ob run myapi --mcp --transport stdio --profile prod
ob myapi --mcp --transport sse --port 8080
# Equivalent with a shim:
myapi --mcp
```

Standard mode exposes API tools. Enable `safety.progressive_disclosure: true` to expose the three meta-tools instead. Their names are scoped by the PascalCase app name — `Search<App>Tools`, `Load<App>Tool`, `Invoke<App>Tool` — for example `SearchPetstoreTools`, `LoadPetstoreTool`, and `InvokePetstoreTool` for the app `petstore`. Read the exact names from the MCP `tools/list` response. For predicate queries such as `MethodIs("GET") && PathContains("/users")`, select `safety.search_engine: predicate`.

Example profile safety configuration:

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
      progressive_disclosure: true
      search_engine: predicate
```

- Read-only mode exposes GET operations only. Allow/deny lists use operation IDs.
- A tool call requiring confirmation must include `_confirm: true`; confirmation is not remembered.
- Rate limits apply per process; dangerous-operation regexes match `METHOD path` and tool ID and reject matches even with confirmation.
- Unreleased registry construction filters first, then rejects remaining tool-name collisions with method/path/operationId diagnostics. Unambiguous names are preserved. Use clean, unique `operationId` values supported by the existing naming rules and rediscover tools; no new naming extension is available.
- Unreleased MCP requests validate parameters, apply profile headers/query overrides like CLI, and carry call cancellation through HTTP/retry waits. SSE follows the server context when shutting down.

## Configuration and caching

Set `OPENBRIDGE_CONFIG_DIR` to override the full config directory. Otherwise:

| Platform | Directory |
| --- | --- |
| macOS | `~/Library/Application Support/openbridge/` |
| Linux/Unix | `$XDG_CONFIG_HOME/openbridge/`, or `~/.config/openbridge/` when unset |
| Windows | `%APPDATA%\openbridge\` (falls back to the user's `AppData\Roaming` when unset) |

App configuration lives in `apps/<app>.yaml`. Profiles can tune HTTP behavior:

```yaml
profiles:
  prod:
    name: prod
    base_url: https://api.example.com
    headers:
      X-Environment: prod
    query_params:
      tenant: example
    timeout: 30s
    tls:
      ca_file: /etc/ssl/private-ca.pem
      cert_file: /etc/ssl/client.pem
      key_file: /etc/ssl/client-key.pem
      server_name: api.example.com
      min_version: "1.2"
      insecure_skip_verify: false
    retry:
      max_retries: 3
      initial_delay: 200ms
      max_delay: 2s
      retryable_status_codes: [429, 503]
    protect_sensitive_info: true
```

Timeout defaults to `30s`. Only supported idempotent methods (`GET`, `HEAD`, `PUT`, `DELETE`, `OPTIONS`) are retried; bodies are replayed. Profile headers/query values override the same request keys after credential injection, preserving unrelated values. Unreleased MCP uses this same layering. Do not put credentials directly in configuration headers or query values.

`spec-cache` stores remote spec bytes and revalidation metadata; removing it also removes the offline fallback until refetched. `index-cache` contains versioned derived data and rebuilds automatically, including the Unreleased schema **4 → 5** change. App YAML and credentials are not caches and must be preserved. See [architecture](/reference/architecture#caches-and-versions) for validity keys and fallbacks.

## OpenAPI extensions

The existing CLI mapping extensions customize resource and verb names:

```yaml
paths:
  /server/reboot:
    post:
      x-cli-verb: trigger
      x-cli-resource: server
```

These CLI mapping options are not a new MCP tool-naming contract. Check the installed spec's help/tool discovery after changes.
