import { useEffect, useMemo, useRef, useState } from "react";
import { log } from "../lib/log";

type Todo = { id: number; title: string; done: boolean };
type Filter = "all" | "active" | "done";

function TodoItem(props: {
  todo: Todo;
  onToggle: (id: number) => void;
  onRename: (id: number, title: string) => void;
  onDelete: (id: number) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(props.todo.title);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing) input.current?.focus();
  }, [editing]);

  const commit = () => {
    const t = draft.trim();
    if (t) props.onRename(props.todo.id, t);
    else setDraft(props.todo.title);
    setEditing(false);
  };

  return (
    <li className={props.todo.done ? "todo done" : "todo"} data-id={props.todo.id}>
      <input type="checkbox" className="toggle" checked={props.todo.done} onChange={() => props.onToggle(props.todo.id)} />
      {editing ? (
        <input
          ref={input}
          className="edit"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") commit();
            if (e.key === "Escape") {
              setDraft(props.todo.title);
              setEditing(false);
            }
          }}
          onBlur={commit}
        />
      ) : (
        <span className="title" onDoubleClick={() => setEditing(true)}>
          {props.todo.title}
        </span>
      )}
      <button className="delete" onClick={() => props.onDelete(props.todo.id)}>
        x
      </button>
    </li>
  );
}

// Todo list: add, toggle, inline edit, delete, filter, clear completed.
export default function AppTodo() {
  const [todos, setTodos] = useState<Todo[]>([
    { id: 1, title: "write tests", done: false },
    { id: 2, title: "ship it", done: false },
  ]);
  const [filter, setFilter] = useState<Filter>("all");
  const [title, setTitle] = useState("");
  const nextId = useRef(3);

  const visible = useMemo(
    () => todos.filter((t) => (filter === "all" ? true : filter === "done" ? t.done : !t.done)),
    [todos, filter],
  );
  const left = todos.filter((t) => !t.done).length;

  useEffect(() => {
    log("todos", todos.length, "left", left);
  }, [todos, left]);

  return (
    <div className="todoapp">
      <form
        id="new"
        onSubmit={(e) => {
          e.preventDefault();
          if (!title.trim()) return;
          setTodos([...todos, { id: nextId.current++, title: title.trim(), done: false }]);
          setTitle("");
        }}
      >
        <input id="new-title" placeholder="What needs doing?" value={title} onChange={(e) => setTitle(e.target.value)} />
      </form>
      <ul id="list">
        {visible.map((t) => (
          <TodoItem
            key={t.id}
            todo={t}
            onToggle={(id) => setTodos(todos.map((x) => (x.id === id ? { ...x, done: !x.done } : x)))}
            onRename={(id, nt) => setTodos(todos.map((x) => (x.id === id ? { ...x, title: nt } : x)))}
            onDelete={(id) => setTodos(todos.filter((x) => x.id !== id))}
          />
        ))}
      </ul>
      <footer>
        <span id="left">{left} left</span>
        {(["all", "active", "done"] as Filter[]).map((f) => (
          <button key={f} id={"filter-" + f} className={filter === f ? "selected" : ""} onClick={() => setFilter(f)}>
            {f}
          </button>
        ))}
        {todos.some((t) => t.done) && (
          <button id="clear" onClick={() => setTodos(todos.filter((t) => !t.done))}>
            clear completed
          </button>
        )}
      </footer>
    </div>
  );
}
