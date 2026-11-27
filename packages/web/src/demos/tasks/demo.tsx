import { createSignal, createMemo } from "@krate/runtime";
import "./demo.css";

type Task = { id: number; text: string; col: "todo" | "doing" | "done" };
const COLS = ["todo", "doing", "done"];

const seed: Task[] = [
  { id: 1, text: "Sketch the page", col: "done" },
  { id: 2, text: "Write the component", col: "doing" },
  { id: 3, text: "Ship it", col: "todo" },
];

// persist keeps the board in localStorage, so it survives a reload and syncs
// between tabs. Cards morph between columns with the View Transitions API.
export default function Demo() {
  const [tasks, setTasks] = createSignal<Task[]>(seed, { persist: "krate-demo-tasks" });
  const [draft, setDraft] = createSignal("");

  const todo = createMemo(() => tasks().filter((t) => t.col === "todo"));
  const doing = createMemo(() => tasks().filter((t) => t.col === "doing"));
  const done = createMemo(() => tasks().filter((t) => t.col === "done"));

  // Runs the update inside a view transition when the browser supports it.
  const animate = (update: () => void) => {
    const calm = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const start = (document as any).startViewTransition;
    if (start && !calm) start.call(document, update);
    else update();
  };

  const add = (e: Event) => {
    e.preventDefault();
    const text = draft().trim();
    if (!text) return;
    animate(() => setTasks([...tasks(), { id: Date.now(), text, col: "todo" }]));
    setDraft("");
  };
  const move = (id: number, by: number) =>
    animate(() =>
      setTasks(tasks().map((t) => (t.id === id ? { ...t, col: COLS[COLS.indexOf(t.col) + by] as Task["col"] } : t))),
    );
  const remove = (id: number) => animate(() => setTasks(tasks().filter((t) => t.id !== id)));

  return (
    <div class="tk">
      <form class="tk-add dk-row" onSubmit={add}>
        <input class="dk-input" aria-label="New task" placeholder="Add a task" value={draft()} onInput={(e) => setDraft((e.target as HTMLInputElement).value)} />
        <button type="submit" class="dk-btn dk-btn-primary">Add</button>
        <button type="button" class="dk-btn" onClick={() => animate(() => setTasks(seed))}>Reset</button>
      </form>

      <div class="tk-board">
        <section class="tk-col" aria-label="To do">
          <h2>To do <span class="tk-n">{todo().length}</span></h2>
          <ul>
            {todo().map((t) => (
              <li key={t.id} class="tk-card" style={"view-transition-name: tk" + t.id}>
                <span>{t.text}</span>
                <span class="tk-actions">
                  <button type="button" aria-label={"Start " + t.text} onClick={() => move(t.id, 1)}>&rarr;</button>
                  <button type="button" aria-label={"Delete " + t.text} onClick={() => remove(t.id)}>&times;</button>
                </span>
              </li>
            ))}
          </ul>
        </section>
        <section class="tk-col" aria-label="Doing">
          <h2>Doing <span class="tk-n">{doing().length}</span></h2>
          <ul>
            {doing().map((t) => (
              <li key={t.id} class="tk-card" style={"view-transition-name: tk" + t.id}>
                <span>{t.text}</span>
                <span class="tk-actions">
                  <button type="button" aria-label={"Move " + t.text + " back to to do"} onClick={() => move(t.id, -1)}>&larr;</button>
                  <button type="button" aria-label={"Finish " + t.text} onClick={() => move(t.id, 1)}>&rarr;</button>
                  <button type="button" aria-label={"Delete " + t.text} onClick={() => remove(t.id)}>&times;</button>
                </span>
              </li>
            ))}
          </ul>
        </section>
        <section class="tk-col" aria-label="Done">
          <h2>Done <span class="tk-n">{done().length}</span></h2>
          <ul>
            {done().map((t) => (
              <li key={t.id} class="tk-card tk-done" style={"view-transition-name: tk" + t.id}>
                <span>{t.text}</span>
                <span class="tk-actions">
                  <button type="button" aria-label={"Reopen " + t.text} onClick={() => move(t.id, -1)}>&larr;</button>
                  <button type="button" aria-label={"Delete " + t.text} onClick={() => remove(t.id)}>&times;</button>
                </span>
              </li>
            ))}
          </ul>
        </section>
      </div>
      <p class="dk-muted tk-hint">Reload the page: the board comes back from localStorage.</p>
    </div>
  );
}
