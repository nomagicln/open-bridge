# Todo Service Example

This is a feature-rich FastAPI application demonstrating complex nested data structures, managed by [uv](https://github.com/astral-sh/uv).

## Features

This Todo service showcases:

- **Complex Enums**: Priority (low, medium, high, critical), Status (pending, in_progress, completed, blocked, cancelled), RecurrenceType
- **DateTime Fields**: Due dates, reminders, timestamps with timezone support
- **Nested Objects**: Subtasks, Labels (with colors), Reminders, Attachments, Metadata
- **Advanced Queries**: Filtering by status/priority, text search, pagination, sorting
- **Batch Operations**: Create or delete multiple todos at once
- **Statistics**: Aggregated metrics and counts

## Prerequisites

- [uv](https://github.com/astral-sh/uv) installed.

## Setup & Run

Initialize the environment and run the server:

```bash
# Install dependencies
uv sync

# Run the server
uv run uvicorn main:app --reload
```

The service will be available at `http://localhost:8000`.

## API Documentation

Once running, you can access the interactive API docs at:
- Swagger UI: `http://localhost:8000/docs`
- ReDoc: `http://localhost:8000/redoc`

## API Endpoints

### CRUD Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/todos` | Create a new todo with nested structures |
| GET | `/todos` | List todos with filtering and pagination |
| GET | `/todos/{todo_id}` | Get a specific todo |
| PUT | `/todos/{todo_id}` | Update a todo |
| DELETE | `/todos/{todo_id}` | Delete a todo |

### Batch Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/todos/batch` | Create multiple todos (up to 100) |
| DELETE | `/todos/batch` | Delete multiple todos by IDs |

### Statistics

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/todos/stats` | Get aggregated statistics |

## Example Usage

### Create a Todo with Complex Data

```bash
curl -X POST http://localhost:8000/todos \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Complete project documentation",
    "description": "Write comprehensive API docs",
    "priority": "high",
    "status": "in_progress",
    "due_date": "2025-02-01T17:00:00Z",
    "labels": [
      {"name": "documentation", "color": "#3B82F6"},
      {"name": "urgent", "color": "#EF4444"}
    ],
    "subtasks": [
      {"title": "Write API overview"},
      {"title": "Add code examples"},
      {"title": "Review and polish"}
    ],
    "reminders": [
      {"remind_at": "2025-01-31T09:00:00Z", "notification_type": "email"}
    ],
    "estimated_minutes": 240,
    "assignee_ids": ["user-123", "user-456"]
  }'
```

### List Todos with Filtering

```bash
# Filter by status and priority
curl "http://localhost:8000/todos?status=in_progress&priority=high&page=1&page_size=10"

# Search and sort
curl "http://localhost:8000/todos?search=documentation&sort_by=due_date&sort_order=asc"
```

### Batch Create Todos

```bash
curl -X POST http://localhost:8000/todos/batch \
  -H "Content-Type: application/json" \
  -d '{
    "items": [
      {"title": "Task 1", "priority": "low"},
      {"title": "Task 2", "priority": "medium"},
      {"title": "Task 3", "priority": "high"}
    ]
  }'
```

### Get Statistics

```bash
curl http://localhost:8000/todos/stats
```

## Data Models

### Todo

The main Todo object includes:

```json
{
  "id": 1,
  "title": "Example todo",
  "description": "Detailed description",
  "priority": "high",
  "status": "in_progress",
  "due_date": "2025-02-01T17:00:00Z",
  "labels": [{"name": "work", "color": "#3B82F6", "description": null}],
  "subtasks": [{"id": "uuid", "title": "Subtask", "completed": false, "completed_at": null}],
  "reminders": [{"remind_at": "2025-01-31T09:00:00Z", "notification_type": "push", "message": null}],
  "recurrence": {"type": "weekly", "interval": 1, "end_date": null, "occurrences": null},
  "attachments": [{"id": "uuid", "filename": "doc.pdf", "mime_type": "application/pdf", "size_bytes": 1024, "url": "https://..."}],
  "parent_id": null,
  "assignee_ids": ["user-123"],
  "estimated_minutes": 60,
  "actual_minutes": null,
  "metadata": {
    "created_at": "2025-01-15T10:00:00Z",
    "updated_at": "2025-01-15T10:00:00Z",
    "created_by": null,
    "version": 1,
    "tags": ["important"],
    "custom_fields": {"client": "ACME Corp"}
  },
  "completed_at": null,
  "progress_percent": 0
}
```

## Project Structure

- `main.py`: Application code with all models and endpoints.
- `pyproject.toml`: Project configuration and dependencies.
- `uv.lock`: Locked dependencies.
