import { createSignal } from '@krate/runtime';
import { List, Add, Toggle, Watch, Count } from '../lib/tasks.go';
import { greet, addNote, noteCount } from '../lib/notes.host';

export default function Home() {
  const [title, setTitle] = createSignal('');
  const [greeting, setGreeting] = createSignal('');
  const [seen, setSeen] = createSignal(0);

  // Host functions are async: call them from handlers and effects. Reading a
  // host signal such as Count() in JSX stays up to date by itself.
  const add = async () => {
    if (!title()) return;
    await Add(title());
    setTitle('');
  };
  const watch = async () => {
    for await (const n of Watch()) setSeen(n);
  };

  return (
    <main>
      <h1>Native Tasks</h1>
      <p>Open tasks: {Count()}</p>
      <p>Notes: {noteCount()}</p>
      <p>Tasks seen by the stream: {seen()}</p>
      <input value={title()} onInput={(e) => setTitle(e.currentTarget.value)} />
      <button onClick={add}>Add</button>
      <button onClick={watch}>Watch</button>
      <button onClick={async () => setGreeting(await greet('world'))}>Greet</button>
      <button onClick={() => addNote('hello')}>Add note</button>
      <p>{greeting()}</p>
      <button
        onClick={async () => {
          const all = await List();
          if (all[0]) await Toggle(all[0].id);
        }}
      >
        Toggle first
      </button>
      <a href="/tasks/1">Task 1</a>
    </main>
  );
}
