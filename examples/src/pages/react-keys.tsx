import { Fragment, useEffect, useState } from 'react';

// Keys and list reconciliation: rows keep their state (inputs, focus,
// component state, effects) when the list is reordered, filtered or has items
// inserted or removed, because they are matched by key.

type Todo = { id: number, title: string };

function TodoRow(props: { todo: Todo, index: number, onRemove: (id: number) => void, onLog: (line: string) => void }) {
  const [draft, setDraft] = useState(props.todo.title);
  const [clicks, setClicks] = useState(0);
  useEffect(() => {
    props.onLog('mount ' + props.todo.id);
    return () => props.onLog('unmount ' + props.todo.id);
  }, []);
  return (
    <li data-testid="todo-row" data-id={props.todo.id} class="flex items-center gap-2 py-1">
      <span class="pos w-6 text-slate-500">{props.index}</span>
      <input
        class="draft rounded border border-slate-700 bg-slate-950 px-2 py-1 text-sm"
        value={draft}
        onChange={(e: any) => setDraft(e.target.value)}
      />
      <button class="clicks rounded bg-slate-800 px-2 py-1 text-sm" onClick={() => setClicks(clicks + 1)}>
        {clicks}
      </button>
      <span class="title text-sm text-slate-300">{props.todo.title}</span>
      <button class="remove text-sm text-red-400" onClick={() => props.onRemove(props.todo.id)}>
        remove
      </button>
    </li>
  );
}

function Detail(props: { id: string, onLog: (line: string) => void }) {
  useEffect(() => {
    props.onLog('detail open ' + props.id);
    return () => props.onLog('detail close ' + props.id);
  }, []);
  return <p data-testid="detail">details for {props.id}</p>;
}

function Section(props: { id: string, open: boolean, onToggle: (id: string) => void, onLog: (line: string) => void }) {
  const [hits, setHits] = useState(0);
  const label = props.id.toUpperCase() + ' x' + hits;
  return (
    <div data-section={props.id}>
      <button
        class="toggle"
        onClick={() => {
          setHits(hits + 1);
          props.onToggle(props.id);
        }}
      >
        {label}
      </button>
      {props.open && <Detail id={props.id} onLog={props.onLog} />}
    </div>
  );
}

function Profile(props: { user: string }) {
  const [note, setNote] = useState('');
  return (
    <label data-testid="profile" data-user={props.user} class="flex items-center gap-2">
      <span class="text-sm">Note for {props.user}</span>
      <input
        data-testid="profile-note"
        class="rounded border border-slate-700 bg-slate-950 px-2 py-1 text-sm"
        value={note}
        onChange={(e: any) => setNote(e.target.value)}
      />
    </label>
  );
}

export default function Page() {
  const [todos, setTodos] = useState<Todo[]>([
    { id: 1, title: 'one' },
    { id: 2, title: 'two' },
    { id: 3, title: 'three' },
  ]);
  const [log, setLog] = useState<string[]>([]);
  const [nextId, setNextId] = useState(4);
  const [user, setUser] = useState('ada');
  const [groups, setGroups] = useState([
    { id: 'fruit', items: ['apple', 'pear'] },
    { id: 'veg', items: ['leek'] },
  ]);

  const addLog = (line: string) => setLog((prev) => [...prev, line]);
  const [openSection, setOpenSection] = useState('');
  const sections = ['alpha', 'beta'];
  const remove = (id: number) => setTodos(todos.filter((t) => t.id !== id));
  const insertMiddle = () => {
    const middle = Math.floor(todos.length / 2);
    setTodos([...todos.slice(0, middle), { id: nextId, title: 'new ' + nextId }, ...todos.slice(middle)]);
    setNextId(nextId + 1);
  };
  const rename = () => setTodos(todos.map((t) => (t.id === 2 ? { ...t, title: t.title + '!' } : t)));

  return (
    <div class="mx-auto max-w-3xl space-y-6 px-6 py-10">
      <header class="space-y-2">
        <p class="text-xs font-semibold uppercase tracking-wide text-indigo-300">Conformance corpus</p>
        <h1 class="text-3xl font-bold text-slate-100">Keys and lists</h1>
      </header>

      <div class="flex flex-wrap gap-2">
        <button data-testid="reverse" onClick={() => setTodos([...todos].reverse())}>reverse</button>
        <button data-testid="rotate" onClick={() => setTodos([...todos.slice(1), todos[0]])}>rotate</button>
        <button data-testid="insert" onClick={insertMiddle}>insert middle</button>
        <button data-testid="rename" onClick={rename}>rename two</button>
        <button data-testid="clear" onClick={() => setTodos([])}>clear</button>
        <button data-testid="restore" onClick={() => setTodos([{ id: 1, title: 'one' }, { id: 2, title: 'two' }, { id: 3, title: 'three' }])}>restore</button>
      </div>

      <ul data-testid="todos">
        {todos.map((todo, i) => (
          <TodoRow key={todo.id} todo={todo} index={i} onRemove={remove} onLog={addLog} />
        ))}
      </ul>
      {todos.length === 0 ? <p data-testid="empty">Nothing to do.</p> : null}

      <ul data-testid="plain">
        {todos.map((todo) => (
          <li key={todo.id}>
            {todo.title}
            <input class="plain-input" />
          </li>
        ))}
      </ul>

      <dl data-testid="pairs">
        {todos.map((todo) => (
          <Fragment key={todo.id}>
            <dt>{todo.id}</dt>
            <dd>{todo.title}</dd>
          </Fragment>
        ))}
      </dl>

      <table>
        <tbody data-testid="table">
          {todos.map((todo, i) => (
            <tr key={todo.id}>
              <td>{i}</td>
              <td>{todo.title}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <select data-testid="picker">
        {todos.map((todo) => (
          <option key={todo.id} value={todo.id}>
            {todo.title}
          </option>
        ))}
      </select>

      <p data-testid="tags">{todos.map((todo) => todo.title + ';')}</p>

      <div data-testid="groups">
        {groups.map((group) => (
          <section key={group.id} data-group={group.id}>
            <h3>{group.id}</h3>
            <ul>
              {group.items.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </section>
        ))}
      </div>
      <button data-testid="groups-swap" onClick={() => setGroups([{ id: 'veg', items: ['leek', 'kale'] }, groups[0]])}>
        swap groups
      </button>

      <div class="flex items-center gap-2">
        <button data-testid="user-ada" onClick={() => setUser('ada')}>ada</button>
        <button data-testid="user-grace" onClick={() => setUser('grace')}>grace</button>
      </div>
      <Profile key={user} user={user} />

      <div data-testid="sections">
        {sections.map((id) => (
          <Section
            key={id}
            id={id}
            open={openSection === id}
            onToggle={(next) => setOpenSection(openSection === next ? '' : next)}
            onLog={addLog}
          />
        ))}
      </div>

      <ol data-testid="log" class="text-xs text-slate-500">
        {log.map((line, i) => (
          <li key={i}>{line}</li>
        ))}
      </ol>
    </div>
  );
}
