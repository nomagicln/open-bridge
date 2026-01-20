from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import List, Optional

app = FastAPI()

class Todo(BaseModel):
    id: int
    title: str
    description: Optional[str] = None
    completed: bool = False

class TodoCreate(BaseModel):
    title: str
    description: Optional[str] = None
    completed: bool = False

class TodoUpdate(BaseModel):
    title: Optional[str] = None
    description: Optional[str] = None
    completed: Optional[bool] = None

# In-memory storage
todos: dict[int, Todo] = {}
current_id = 0

@app.post("/todos", response_model=Todo, status_code=201)
def create_todo(todo: TodoCreate):
    global current_id
    current_id += 1
    new_todo = Todo(id=current_id, **todo.model_dump())
    todos[current_id] = new_todo
    return new_todo

@app.get("/todos", response_model=List[Todo])
def list_todos():
    return list(todos.values())

@app.get("/todos/{todo_id}", response_model=Todo)
def get_todo(todo_id: int):
    if todo_id not in todos:
        raise HTTPException(status_code=404, detail="Todo not found")
    return todos[todo_id]

@app.put("/todos/{todo_id}", response_model=Todo)
def update_todo(todo_id: int, todo_update: TodoUpdate):
    if todo_id not in todos:
        raise HTTPException(status_code=404, detail="Todo not found")
    
    stored_todo = todos[todo_id]
    update_data = todo_update.model_dump(exclude_unset=True)
    updated_todo = stored_todo.model_copy(update=update_data)
    todos[todo_id] = updated_todo
    return updated_todo

@app.delete("/todos/{todo_id}", status_code=204)
def delete_todo(todo_id: int):
    if todo_id not in todos:
        raise HTTPException(status_code=404, detail="Todo not found")
    del todos[todo_id]
    return

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
