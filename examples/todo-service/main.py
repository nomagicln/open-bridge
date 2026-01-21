"""
Todo Service - A feature-rich TODO API demonstrating complex nested data structures.

This service showcases:
- Complex enums (Priority, Status, RecurrenceType)
- DateTime fields with timezone support
- Nested objects (Subtask, Label, Reminder, Metadata)
- Complex filtering and pagination
- Batch operations
"""

from datetime import datetime, timedelta, timezone
from enum import Enum
from typing import Annotated
from uuid import uuid4

from fastapi import FastAPI, HTTPException, Query
from pydantic import BaseModel, Field


# =============================================================================
# Enums - Complex data types for categorization
# =============================================================================


class Priority(str, Enum):
    """Priority levels for todos."""

    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


class Status(str, Enum):
    """Status of a todo item."""

    PENDING = "pending"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    BLOCKED = "blocked"
    CANCELLED = "cancelled"


class RecurrenceType(str, Enum):
    """Recurrence patterns for recurring todos."""

    NONE = "none"
    DAILY = "daily"
    WEEKLY = "weekly"
    MONTHLY = "monthly"
    YEARLY = "yearly"


# =============================================================================
# Nested Models - Complex nested data structures
# =============================================================================


class Label(BaseModel):
    """Label for categorizing todos."""

    name: str = Field(..., min_length=1, max_length=50, description="Label name")
    color: str = Field(
        default="#808080", pattern=r"^#[0-9A-Fa-f]{6}$", description="Hex color code"
    )
    description: str | None = Field(default=None, description="Label description")


class Subtask(BaseModel):
    """A subtask within a todo."""

    id: str = Field(default_factory=lambda: str(uuid4()), description="Subtask UUID")
    title: str = Field(..., min_length=1, max_length=200, description="Subtask title")
    completed: bool = Field(default=False, description="Completion status")
    completed_at: datetime | None = Field(
        default=None, description="Completion timestamp"
    )


class Reminder(BaseModel):
    """Reminder configuration for a todo."""

    remind_at: datetime = Field(..., description="When to send the reminder")
    notification_type: str = Field(
        default="push", description="Notification type: push, email, sms"
    )
    message: str | None = Field(default=None, description="Custom reminder message")


class Recurrence(BaseModel):
    """Recurrence configuration for repeating todos."""

    type: RecurrenceType = Field(
        default=RecurrenceType.NONE, description="Recurrence pattern"
    )
    interval: int = Field(default=1, ge=1, le=365, description="Interval between occurrences")
    end_date: datetime | None = Field(
        default=None, description="When recurrence ends"
    )
    occurrences: int | None = Field(
        default=None, ge=1, description="Maximum number of occurrences"
    )


class Metadata(BaseModel):
    """Metadata about the todo item."""

    created_at: datetime = Field(
        default_factory=lambda: datetime.now(timezone.utc),
        description="Creation timestamp",
    )
    updated_at: datetime = Field(
        default_factory=lambda: datetime.now(timezone.utc),
        description="Last update timestamp",
    )
    created_by: str | None = Field(default=None, description="Creator user ID")
    version: int = Field(default=1, ge=1, description="Version number for optimistic locking")
    tags: list[str] = Field(default_factory=list, description="Free-form tags")
    custom_fields: dict[str, str | int | bool | None] = Field(
        default_factory=dict, description="Custom key-value pairs"
    )


class Attachment(BaseModel):
    """File attachment for a todo."""

    id: str = Field(default_factory=lambda: str(uuid4()), description="Attachment UUID")
    filename: str = Field(..., min_length=1, description="Original filename")
    mime_type: str = Field(..., description="MIME type of the file")
    size_bytes: int = Field(..., ge=0, description="File size in bytes")
    url: str = Field(..., description="Download URL")


# =============================================================================
# Main Todo Models - Request and Response schemas
# =============================================================================


class TodoBase(BaseModel):
    """Base todo fields shared across create/update operations."""

    title: str = Field(
        ..., min_length=1, max_length=500, description="Todo title"
    )
    description: str | None = Field(
        default=None, max_length=5000, description="Detailed description"
    )
    priority: Priority = Field(default=Priority.MEDIUM, description="Priority level")
    status: Status = Field(default=Status.PENDING, description="Current status")
    due_date: datetime | None = Field(default=None, description="Due date and time")
    labels: list[Label] = Field(default_factory=list, description="Categorization labels")
    subtasks: list[Subtask] = Field(default_factory=list, description="Child subtasks")
    reminders: list[Reminder] = Field(
        default_factory=list, description="Scheduled reminders"
    )
    recurrence: Recurrence | None = Field(
        default=None, description="Recurrence configuration"
    )
    attachments: list[Attachment] = Field(
        default_factory=list, description="File attachments"
    )
    parent_id: int | None = Field(
        default=None, description="Parent todo ID for nested todos"
    )
    assignee_ids: list[str] = Field(
        default_factory=list, description="Assigned user IDs"
    )
    estimated_minutes: int | None = Field(
        default=None, ge=1, le=10080, description="Estimated time in minutes (max 1 week)"
    )
    actual_minutes: int | None = Field(
        default=None, ge=0, description="Actual time spent in minutes"
    )


class TodoCreate(TodoBase):
    """Request body for creating a new todo."""

    pass


class TodoUpdate(BaseModel):
    """Request body for updating an existing todo (all fields optional)."""

    title: str | None = Field(
        default=None, min_length=1, max_length=500, description="Todo title"
    )
    description: str | None = Field(
        default=None, max_length=5000, description="Detailed description"
    )
    priority: Priority | None = Field(default=None, description="Priority level")
    status: Status | None = Field(default=None, description="Current status")
    due_date: datetime | None = Field(default=None, description="Due date and time")
    labels: list[Label] | None = Field(default=None, description="Categorization labels")
    subtasks: list[Subtask] | None = Field(default=None, description="Child subtasks")
    reminders: list[Reminder] | None = Field(
        default=None, description="Scheduled reminders"
    )
    recurrence: Recurrence | None = Field(
        default=None, description="Recurrence configuration"
    )
    attachments: list[Attachment] | None = Field(
        default=None, description="File attachments"
    )
    parent_id: int | None = Field(
        default=None, description="Parent todo ID for nested todos"
    )
    assignee_ids: list[str] | None = Field(
        default=None, description="Assigned user IDs"
    )
    estimated_minutes: int | None = Field(
        default=None, ge=1, le=10080, description="Estimated time in minutes"
    )
    actual_minutes: int | None = Field(
        default=None, ge=0, description="Actual time spent in minutes"
    )


class Todo(TodoBase):
    """Complete todo item with all fields (response model)."""

    id: int = Field(..., description="Unique todo ID")
    metadata: Metadata = Field(
        default_factory=Metadata, description="Todo metadata"
    )
    completed_at: datetime | None = Field(
        default=None, description="Completion timestamp"
    )
    progress_percent: int = Field(
        default=0, ge=0, le=100, description="Progress percentage"
    )


# =============================================================================
# Response Models for Pagination and Batch Operations
# =============================================================================


class PaginatedResponse(BaseModel):
    """Paginated list response with metadata."""

    items: list[Todo] = Field(..., description="List of todo items")
    total: int = Field(..., ge=0, description="Total number of items")
    page: int = Field(..., ge=1, description="Current page number")
    page_size: int = Field(..., ge=1, le=100, description="Items per page")
    total_pages: int = Field(..., ge=0, description="Total number of pages")
    has_next: bool = Field(..., description="Whether there are more pages")
    has_previous: bool = Field(..., description="Whether there are previous pages")


class BatchCreateRequest(BaseModel):
    """Request body for batch creating multiple todos."""

    items: list[TodoCreate] = Field(
        ..., min_length=1, max_length=100, description="Todos to create (max 100)"
    )


class BatchCreateResponse(BaseModel):
    """Response for batch create operation."""

    created: list[Todo] = Field(..., description="Successfully created todos")
    failed: list[dict[str, str | int]] = Field(
        ..., description="Failed items with error details"
    )
    total_created: int = Field(..., ge=0, description="Number of successfully created todos")
    total_failed: int = Field(..., ge=0, description="Number of failed creations")


class BatchDeleteRequest(BaseModel):
    """Request body for batch deleting multiple todos."""

    ids: list[int] = Field(
        ..., min_length=1, max_length=100, description="Todo IDs to delete (max 100)"
    )


class BatchDeleteResponse(BaseModel):
    """Response for batch delete operation."""

    deleted_ids: list[int] = Field(..., description="Successfully deleted IDs")
    not_found_ids: list[int] = Field(..., description="IDs that were not found")
    total_deleted: int = Field(..., ge=0, description="Number of successfully deleted todos")


class StatsResponse(BaseModel):
    """Statistics about todos."""

    total_todos: int = Field(..., ge=0, description="Total number of todos")
    by_status: dict[str, int] = Field(..., description="Count by status")
    by_priority: dict[str, int] = Field(..., description="Count by priority")
    overdue_count: int = Field(..., ge=0, description="Number of overdue todos")
    completed_this_week: int = Field(
        ..., ge=0, description="Todos completed this week"
    )
    average_completion_time_minutes: float | None = Field(
        default=None, description="Average time to complete todos"
    )


# =============================================================================
# FastAPI Application
# =============================================================================


app = FastAPI(
    title="Todo Service API",
    description="""
A feature-rich TODO API demonstrating complex nested data structures.

## Features

- **Priority Levels**: low, medium, high, critical
- **Status Tracking**: pending, in_progress, completed, blocked, cancelled
- **Nested Subtasks**: Break down todos into smaller tasks
- **Labels & Tags**: Organize with custom labels and tags
- **Reminders**: Schedule notifications
- **Recurrence**: Create recurring todos
- **Attachments**: Link files to todos
- **Time Tracking**: Estimate and track actual time spent
- **Batch Operations**: Create or delete multiple todos at once
- **Filtering & Pagination**: Advanced query capabilities
    """,
    version="2.0.0",
    contact={"name": "OpenBridge Team", "url": "https://github.com/nomagicln/open-bridge"},
    servers=[{"url": "http://localhost:8000", "description": "Local development server"}],
)


# In-memory storage
todos: dict[int, Todo] = {}
current_id = 0


def calculate_progress(todo: Todo) -> int:
    """Calculate progress percentage based on subtasks."""
    if not todo.subtasks:
        return 100 if todo.status == Status.COMPLETED else 0
    completed = sum(1 for s in todo.subtasks if s.completed)
    return int((completed / len(todo.subtasks)) * 100)


# =============================================================================
# CRUD Endpoints
# =============================================================================


@app.post("/todos", response_model=Todo, status_code=201, tags=["todos"])
def create_todo(todo: TodoCreate) -> Todo:
    """
    Create a new todo with complex nested structures.

    Supports:
    - Priority and status settings
    - Nested subtasks
    - Labels with colors
    - Reminders
    - Recurrence patterns
    - File attachments
    """
    global current_id
    current_id += 1

    new_todo = Todo(
        id=current_id,
        **todo.model_dump(),
        metadata=Metadata(),
    )
    new_todo.progress_percent = calculate_progress(new_todo)

    todos[current_id] = new_todo
    return new_todo


@app.get("/todos", response_model=PaginatedResponse, tags=["todos"])
def list_todos(
    page: Annotated[int, Query(ge=1, description="Page number")] = 1,
    page_size: Annotated[int, Query(ge=1, le=100, description="Items per page")] = 20,
    status: Annotated[Status | None, Query(description="Filter by status")] = None,
    priority: Annotated[Priority | None, Query(description="Filter by priority")] = None,
    search: Annotated[str | None, Query(max_length=200, description="Search in title and description")] = None,
    has_subtasks: Annotated[bool | None, Query(description="Filter by presence of subtasks")] = None,
    is_overdue: Annotated[bool | None, Query(description="Filter overdue todos")] = None,
    sort_by: Annotated[str, Query(description="Sort field: created_at, due_date, priority")] = "created_at",
    sort_order: Annotated[str, Query(description="Sort order: asc, desc")] = "desc",
) -> PaginatedResponse:
    """
    List todos with filtering, searching, and pagination.

    Supports:
    - Filtering by status, priority
    - Text search in title/description
    - Filter by subtask presence
    - Filter overdue items
    - Sorting by multiple fields
    """
    now = datetime.now(timezone.utc)
    result = list(todos.values())

    # Apply filters
    if status:
        result = [t for t in result if t.status == status]
    if priority:
        result = [t for t in result if t.priority == priority]
    if search:
        search_lower = search.lower()
        result = [
            t
            for t in result
            if search_lower in t.title.lower()
            or (t.description and search_lower in t.description.lower())
        ]
    if has_subtasks is not None:
        result = [t for t in result if bool(t.subtasks) == has_subtasks]
    if is_overdue is not None:
        if is_overdue:
            # Overdue: has a due date, is past due, and not completed
            result = [
                t
                for t in result
                if t.due_date is not None
                and t.due_date < now
                and t.status != Status.COMPLETED
            ]
        else:
            # Not overdue (but with a due date): either due in future or completed.
            # Excludes items without a due date from this filter.
            result = [
                t
                for t in result
                if t.due_date is not None
                and not (t.due_date < now and t.status != Status.COMPLETED)
            ]

    # Sort
    # Use a far-future date as fallback for items without due_date (safer than datetime.max)
    far_future = datetime(9999, 12, 31, tzinfo=timezone.utc)
    priority_order = {p: i for i, p in enumerate(Priority)}
    sort_key_map = {
        "created_at": lambda t: t.metadata.created_at,
        "due_date": lambda t: t.due_date or far_future,
        "priority": lambda t: priority_order[t.priority],
    }
    sort_func = sort_key_map.get(sort_by, sort_key_map["created_at"])
    result.sort(key=sort_func, reverse=(sort_order == "desc"))

    # Paginate
    total = len(result)
    total_pages = (total + page_size - 1) // page_size if total > 0 else 0
    start = (page - 1) * page_size
    end = start + page_size
    items = result[start:end]

    return PaginatedResponse(
        items=items,
        total=total,
        page=page,
        page_size=page_size,
        total_pages=total_pages,
        has_next=page < total_pages,
        has_previous=page > 1,
    )


# =============================================================================
# Statistics Endpoint (before {todo_id} routes to avoid path conflicts)
# =============================================================================


@app.get("/todos/stats", response_model=StatsResponse, tags=["stats"])
def get_stats() -> StatsResponse:
    """
    Get statistics about all todos.

    Returns counts by status, priority, and other metrics.
    """
    now = datetime.now(timezone.utc)
    week_ago = now - timedelta(days=7)

    all_todos = list(todos.values())

    by_status = {s.value: 0 for s in Status}
    by_priority = {p.value: 0 for p in Priority}
    overdue_count = 0
    completed_this_week = 0
    completion_times: list[float] = []

    for todo in all_todos:
        by_status[todo.status.value] += 1
        by_priority[todo.priority.value] += 1

        if (
            todo.due_date
            and todo.due_date < now
            and todo.status != Status.COMPLETED
        ):
            overdue_count += 1

        if todo.completed_at and todo.completed_at >= week_ago:
            completed_this_week += 1
            # Calculate completion time
            created = todo.metadata.created_at
            if created:
                duration = (todo.completed_at - created).total_seconds() / 60
                completion_times.append(duration)

    avg_time = (
        sum(completion_times) / len(completion_times) if completion_times else None
    )

    return StatsResponse(
        total_todos=len(all_todos),
        by_status=by_status,
        by_priority=by_priority,
        overdue_count=overdue_count,
        completed_this_week=completed_this_week,
        average_completion_time_minutes=avg_time,
    )


# =============================================================================
# Batch Operation Endpoints (before {todo_id} routes to avoid path conflicts)
# =============================================================================


@app.post("/todos/batch", response_model=BatchCreateResponse, tags=["batch"])
def batch_create_todos(request: BatchCreateRequest) -> BatchCreateResponse:
    """
    Create multiple todos in a single request.

    Supports up to 100 items per batch.
    Returns detailed results including any failures.
    """
    global current_id
    created: list[Todo] = []
    failed: list[dict[str, str | int]] = []

    for index, item in enumerate(request.items):
        try:
            current_id += 1
            new_todo = Todo(
                id=current_id,
                **item.model_dump(),
                metadata=Metadata(),
            )
            new_todo.progress_percent = calculate_progress(new_todo)
            todos[current_id] = new_todo
            created.append(new_todo)
        except Exception as e:
            failed.append({"index": index, "error": str(e)})

    return BatchCreateResponse(
        created=created,
        failed=failed,
        total_created=len(created),
        total_failed=len(failed),
    )


@app.delete("/todos/batch", response_model=BatchDeleteResponse, tags=["batch"])
def batch_delete_todos(request: BatchDeleteRequest) -> BatchDeleteResponse:
    """
    Delete multiple todos in a single request.

    Returns lists of successfully deleted and not found IDs.
    """
    deleted_ids: list[int] = []
    not_found_ids: list[int] = []

    for todo_id in request.ids:
        if todo_id in todos:
            del todos[todo_id]
            deleted_ids.append(todo_id)
        else:
            not_found_ids.append(todo_id)

    return BatchDeleteResponse(
        deleted_ids=deleted_ids,
        not_found_ids=not_found_ids,
        total_deleted=len(deleted_ids),
    )


# =============================================================================
# Single Todo Endpoints (with {todo_id} path parameter)
# =============================================================================


@app.get("/todos/{todo_id}", response_model=Todo, tags=["todos"])
def get_todo(todo_id: int) -> Todo:
    """Get a specific todo by ID with all nested data."""
    if todo_id not in todos:
        raise HTTPException(status_code=404, detail="Todo not found")
    return todos[todo_id]


@app.put("/todos/{todo_id}", response_model=Todo, tags=["todos"])
def update_todo(todo_id: int, todo_update: TodoUpdate) -> Todo:
    """
    Update a todo with partial data.

    Supports updating any field including nested structures.
    Uses optimistic locking via metadata.version.
    """
    if todo_id not in todos:
        raise HTTPException(status_code=404, detail="Todo not found")

    stored_todo = todos[todo_id]
    update_data = todo_update.model_dump(exclude_unset=True)

    # Update metadata
    updated_metadata = stored_todo.metadata.model_copy(
        update={
            "updated_at": datetime.now(timezone.utc),
            "version": stored_todo.metadata.version + 1,
        }
    )

    # Handle completed_at transitions based on status changes
    completed_at = stored_todo.completed_at
    if "status" in update_data:
        new_status = update_data["status"]
        if new_status == Status.COMPLETED and stored_todo.status != Status.COMPLETED:
            # Transition to COMPLETED: set completed_at timestamp
            completed_at = datetime.now(timezone.utc)
        elif new_status != Status.COMPLETED and stored_todo.status == Status.COMPLETED:
            # Transition away from COMPLETED: clear completed_at to avoid stale data
            completed_at = None

    updated_todo = stored_todo.model_copy(
        update={**update_data, "metadata": updated_metadata, "completed_at": completed_at}
    )
    updated_todo.progress_percent = calculate_progress(updated_todo)

    todos[todo_id] = updated_todo
    return updated_todo


@app.delete("/todos/{todo_id}", status_code=204, tags=["todos"])
def delete_todo(todo_id: int) -> None:
    """Delete a todo by ID."""
    if todo_id not in todos:
        raise HTTPException(status_code=404, detail="Todo not found")
    del todos[todo_id]


# =============================================================================
# Main Entry Point
# =============================================================================

if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=8000)
