# Todo Service Example

This is a simple FastAPI application managed by [uv](https://github.com/astral-sh/uv).

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

## Project Structure

- `main.py`: Application code.
- `pyproject.toml`: Project configuration and dependencies.
- `uv.lock`: Locked dependencies.
