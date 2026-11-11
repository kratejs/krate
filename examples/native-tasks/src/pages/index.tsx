import { createSignal, createEffect } from '@krate/runtime';
import { List, Add, Toggle, Watch, Count } from '../lib/tasks.go';
import { greet, addNote, noteCount } from '../lib/notes.host';

export default function Home() {
  const [tasks, setTasks] = createSignal([]);
  const [title, setTitle] = createSignal('');
  const [greeting, setGreeting] = createSignal('');
  const [seen, setSeen] = createSignal(0);
  const [error, setError] = createSignal('');

  // Host functions are async: call them from handlers and effects. Reading a
  // host signal such as Count() in JSX stays up to date by itself.
  const refresh = async () => setTasks(await List());
  createEffect(() => {
    refresh();
  });

  const add = async () => {
    if (!title()) return;
    await Add(title());
    setTitle('');
    await refresh();
  };
  const toggle = async (id: number) => {
    await Toggle(id);
    await refresh();
  };
  const toggleMissing = async () => {
    try {
      await Toggle(9999);
    } catch (e) {
      setError(e.code + ': ' + e.message);
    }
  };
  const watch = async () => {
    for await (const n of Watch()) setSeen(n);
  };

  return (
    <main>
      <h1>Native Tasks</h1>
      <p id="open">Open tasks: {Count()}</p>
      <p id="notes">Notes: {noteCount()}</p>
      <p id="seen">Tasks seen by the stream: {seen()}</p>
      <ul id="list">
        {tasks().map((t) => (
          <li key={t.id}>
            <button class="toggle" onClick={() => toggle(t.id)}>
              {t.done ? 'done' : 'open'}
            </button>{' '}
            <Link href={'/tasks/' + t.id}>{t.title}</Link>
          </li>
        ))}
      </ul>
      <input id="title" value={title()} onInput={(e) => setTitle(e.currentTarget.value)} />
      <button id="add" onClick={add}>Add</button>
      <button id="watch" onClick={watch}>Watch</button>
      <button id="greet" onClick={async () => setGreeting(await greet('world'))}>Greet</button>
      <button id="note" onClick={() => addNote('hello')}>Add note</button>
      <button id="missing" onClick={toggleMissing}>Toggle missing</button>
      <p id="greeting">{greeting()}</p>
      <p id="error">{error()}</p>
    </main>
  );
}
