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
| `ob version` | Show version information |
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
# Table output (default)
myapi users list

# JSON output
myapi users list --json

# YAML output
myapi users list --yaml
```

## OpenAPI Extensions

Customize CLI behavior using `x-cli-*` extensions in your OpenAPI spec:

```yaml
paths:
  /server/reboot:
    post:
      x-cli-verb: trigger      # Override default verb mapping
      x-cli-resource: server   # Override resource name
```
