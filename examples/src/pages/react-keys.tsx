import { useEffect, useState } from 'react';

type Todo = { id: number, title: string, done: boolean };

function TodoRow(props: { todo: Todo, index: number, onRemove: (id: number) => void }) {
  const [draft, setDraft] = useState(props.todo.title);
  const [clicks, setClicks] = useState(0);
  useEffect(() => {
    console.log('mount ' + props.todo.id);
    return () => console.log('unmount ' + props.todo.id);
  }, []);
  return (
    <li data-testid="todo-row" data-id={props.todo.id}>
      <span class="pos">{props.index}</span>
      <input class="draft" value={draft} onChange={(e: any) => setDraft(e.target.value)} />
      <button class="clicks" onClick={() => setClicks(clicks + 1)}>{clicks}</button>
      <span class="title">{props.todo.title}</span>
      <button class="remove" onClick={() => props.onRemove(props.todo.id)}>x</button>
    </li>
  );
}

export default function Page() {
  const [todos, setTodos] = useState<Todo[]>([
    { id: 1, title: 'one', done: false },
    { id: 2, title: 'two', done: false },
    { id: 3, title: 'three', done: true },
  ]);
  const remove = (id: number) => setTodos(todos.filter((t) => t.id !== id));
  return (
    <div>
      <button data-testid="reverse" onClick={() => setTodos([...todos].reverse())}>reverse</button>
      <ul data-testid="todos">
        {todos.map((todo, i) => (
          <TodoRow key={todo.id} todo={todo} index={i} onRemove={remove} />
        ))}
      </ul>
      <ul data-testid="plain">
        {todos.map((todo) => (
          <li key={todo.id} class={todo.done ? 'done' : ''}>{todo.title}<input /></li>
        ))}
      </ul>
    </div>
  );
}
