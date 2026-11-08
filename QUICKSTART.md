# OpenBridge Quick Start Guide

This guide walks through building the current source checkout and managing a local Todo service. `main` is Unreleased; `go install ...@latest` currently installs v0.1.3. For an existing integration, read the [upgrade guide](docs/guide/upgrading.md) first.

## Prerequisites

- **Go** (1.26 or later, as declared in `go.mod`): To build the current source checkout.
- **Python** & **[uv](https://github.com/astral-sh/uv)**: To run the example Todo service.

## Step 1: Build OpenBridge

First, build the `ob` binary from the source code.

```bash
# Clone the repository (if you haven't already)
git clone https://github.com/nomagicln/open-bridge.git
cd open-bridge

# Build the binary
make build
```

This will create the `ob` binary in the `bin/` directory. You can add this directory to your `PATH` or use it directly via `./bin/ob`.

Verify the installation:

```bash
./bin/ob --version
```

## Step 2: Start the Example Todo Service

We provide a feature-rich Todo API in the `examples/todo-service` directory. This service demonstrates complex nested data structures including:

- **Priority levels**: low, medium, high, critical
- **Status tracking**: pending, in_progress, completed, blocked, cancelled
- **Nested subtasks**: Break down todos into smaller tasks
- **Labels & tags**: Organize with custom labels and colors
- **Reminders**: Schedule notifications
- **Attachments**: Link files to todos
- **Batch operations**: Create or delete multiple todos at once

Open a new terminal window to run this service:

```bash
cd examples/todo-service

# Install dependencies and run the server
uv sync
uv run uvicorn main:app --reload
```

The service should now be running at `http://localhost:8000`. You can verify it by opening `http://localhost:8000/docs` in your browser to see the interactive API documentation.

## Step 3: Install the Todo App into OpenBridge

Now, switch back to your `open-bridge` root terminal. We will use `ob` to "install" this running service. This process parses the OpenAPI specification exposed by the service and generates a semantic CLI for it.

```bash
./bin/ob install todo --spec http://localhost:8000/openapi.json
```

If successful, you should see a message confirming the installation and the shim path. Interactive mode is opt-in (`./bin/ob install todo -i`); without `-i`, `--spec` is required. For the `todo` shortcut below, add the reported shim directory to `PATH`. You can always use `./bin/ob run todo ...` or `./bin/ob todo ...` instead.

## Step 4: Explore the CLI

OpenBridge dynamically generates commands based on the API's resources. Let's see what's available for our new `todo` app.

```bash
# Show help for the todo app
./bin/ob run todo --help
# OR (if the reported shim directory is on PATH)
todo --help
```

You should see flattened resources including `todos`, `todos-batch`, and `todos-stats` under **Available Resources**. Use the installed spec's help to confirm command names; nested URL segments do not become extra CLI argument levels.

Let's see what we can do with the `todos` resource:

```bash
todo todos --help
```

The `todos` resource provides `list`, `create`, `get`, `update`, and `delete`. Batch operations and statistics belong to the separate resources shown above.

## Step 5: Manage Todos

Now, let's perform some operations using the semantic CLI. Notice the command structure: `<app> <resource> <verb>`.

### Create a Todo with Complex Data

Create a todo with priority, labels, subtasks, and a due date:

```bash
todo todos create \
  --title "Complete project documentation" \
  --description "Write comprehensive API docs with examples" \
  --priority "high" \
  --status "in_progress" \
  --due_date "2025-02-01T17:00:00Z" \
  --labels '[{"name": "documentation", "color": "#3B82F6"}, {"name": "urgent", "color": "#EF4444"}]' \
  --subtasks '[{"title": "Write API overview"}, {"title": "Add code examples"}, {"title": "Review and polish"}]' \
  --estimated_minutes 240
```

### List Todos with Filtering

```bash
# List all todos (paginated)
todo todos list

# Filter by status
todo todos list --status "in_progress"

# Filter by priority and search
todo todos list --priority "high" --search "documentation"

# Sort by due date
todo todos list --sort_by "due_date" --sort_order "asc"
```

### Get a Specific Todo

Replace `1` with the actual ID from the list command if different.

```bash
todo todos get --todo_id 1
```

### Update a Todo

Update the status and add actual time spent:

```bash
todo todos update --todo_id 1 --status "completed" --actual_minutes 180
```

Verify the change:

```bash
todo todos get --todo_id 1
```

### Delete a Todo

```bash
todo todos delete --todo_id 1
```

## Step 6: Batch Operations (Advanced)

The Todo service supports batch operations for efficiency. `/todos/batch` maps to `todos-batch`; the POST path rule maps to `batch`, and DELETE maps to `delete`.

The batch and statistics examples here are derived from `examples/todo-service/main.py` and `pkg/semantic/{resource,verb,mapper}.go`. Runtime smoke verification for the Unreleased revision is tracked in the [architecture audit](docs/maintenance/architecture-audit.md). Use IDs returned by creation in the delete example.

### Batch Create

Create multiple todos at once:

```bash
todo todos-batch batch --items '[
  {"title": "Task 1", "priority": "low"},
  {"title": "Task 2", "priority": "medium"},
  {"title": "Task 3", "priority": "high", "labels": [{"name": "important", "color": "#EF4444"}]}
]'
```

### Batch Delete

Delete multiple todos by IDs:

```bash
todo todos-batch delete --ids '[1, 2, 3]'
```

## Step 7: View Statistics

Get aggregated statistics about all your todos:

```bash
todo todos-stats get
```

This shows counts by status, priority, overdue items, and completion metrics.

## Step 8: Uninstall (Optional)

If you want to remove the `todo` app from OpenBridge:

```bash
ob uninstall todo
```

## Next Steps

- Check out the [README.md](./README.md) for more advanced configuration and features.
- Explore how to use OpenBridge as an [MCP Server](./README.md#3-use-with-ai-mcp-mode) for AI agents.
- See the [Todo Service README](./examples/todo-service/README.md) for detailed API documentation.
