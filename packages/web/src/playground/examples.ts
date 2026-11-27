// Playground examples as plain data. The file is plain JavaScript on purpose
// (no TypeScript syntax): build-playground.mjs loads it as-is to write
// public/playground/examples.json, and site pages can import it directly.
//
// Each example is { id, title, description, tags, entry, open, files }:
//   files  project path -> source text (paths live under src/)
//   entry  the page the preview opens first ("/" or "/about/")
//   open   the file the editor shows first

const base = `:root { color-scheme: light dark; --fg: #171717; --bg: #ffffff; --muted: #737373; --line: #e5e5e5; --accent: #c2410c; --soft: #f5f5f5; }
@media (prefers-color-scheme: dark) {
  :root { --fg: #fafafa; --bg: #0a0a0a; --muted: #a3a3a3; --line: #2a2a2a; --accent: #fb923c; --soft: #171717; }
}
* { box-sizing: border-box; }
body { margin: 0; padding: 2rem 1.25rem; font: 16px/1.5 system-ui, sans-serif; color: var(--fg); background: var(--bg); }
main { max-width: 34rem; margin: 0 auto; }
h1 { font-size: 1.5rem; margin: 0 0 1rem; }
button { font: inherit; cursor: pointer; padding: 0.4rem 0.9rem; border-radius: 8px; border: 1px solid var(--line); background: var(--soft); color: var(--fg); }
button:hover { border-color: var(--accent); }
button:focus-visible, a:focus-visible, input:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
a { color: var(--accent); }
`;

export const examples = [
  {
    id: "counter",
    title: "Counter",
    description: "Signals: createSignal and a click handler.",
    tags: ["signals"],
    entry: "/",
    open: "src/pages/index.tsx",
    files: {
      "src/pages/index.tsx": `import { createSignal } from "@krate/runtime";
import "../styles.css";

export default function Home() {
  const [count, setCount] = createSignal(0);

  return (
    <main>
      <h1>Counter</h1>
      <p class="value">{count()}</p>
      <div class="row">
        <button type="button" onClick={() => setCount((c) => c - 1)}>-</button>
        <button type="button" onClick={() => setCount((c) => c + 1)}>+</button>
        <button type="button" onClick={() => setCount(0)}>Reset</button>
      </div>
    </main>
  );
}
`,
      "src/styles.css":
        base +
        `.value { font-size: 4rem; font-weight: 700; margin: 0.5rem 0; font-variant-numeric: tabular-nums; }
.row { display: flex; gap: 0.5rem; }
`,
    },
  },

  {
    id: "todo",
    title: "Todo list",
    description: "createSignal with persist, createMemo, and a keyed list.",
    tags: ["signals", "lists"],
    entry: "/",
    open: "src/pages/index.tsx",
    files: {
      "src/pages/index.tsx": `import { createSignal, createMemo } from "@krate/runtime";
import "../styles.css";

type Todo = { id: number; text: string; done: boolean };

export default function Todos() {
  // persist saves the list to localStorage and restores it on reload.
  const [todos, setTodos] = createSignal<Todo[]>([], { persist: "playground-todos" });
  const [draft, setDraft] = createSignal("");

  const remaining = createMemo(() => todos().filter((t) => !t.done).length);

  const add = (e: Event) => {
    e.preventDefault();
    const text = draft().trim();
    if (!text) return;
    setTodos([...todos(), { id: Date.now(), text, done: false }]);
    setDraft("");
  };
  const toggle = (id: number) =>
    setTodos(todos().map((t) => (t.id === id ? { ...t, done: !t.done } : t)));
  const remove = (id: number) => setTodos(todos().filter((t) => t.id !== id));

  return (
    <main>
      <h1>Todos</h1>
      <form class="add" onSubmit={add}>
        <input
          aria-label="New todo"
          placeholder="What needs doing?"
          value={draft()}
          onInput={(e) => setDraft((e.target as HTMLInputElement).value)}
        />
        <button type="submit">Add</button>
      </form>
      <ul class="list">
        {todos().map((t) => (
          <li key={t.id} class={t.done ? "done" : ""}>
            <label>
              <input type="checkbox" checked={t.done} onChange={() => toggle(t.id)} />
              <span>{t.text}</span>
            </label>
            <button type="button" aria-label={"Remove " + t.text} onClick={() => remove(t.id)}>x</button>
          </li>
        ))}
      </ul>
      <p class="count">{remaining()} item(s) left</p>
    </main>
  );
}
`,
      "src/styles.css":
        base +
        `.add { display: flex; gap: 0.5rem; margin-bottom: 1rem; }
.add input { flex: 1; font: inherit; padding: 0.4rem 0.7rem; border-radius: 8px; border: 1px solid var(--line); background: var(--bg); color: var(--fg); }
.list { list-style: none; padding: 0; margin: 0; }
.list li { display: flex; justify-content: space-between; align-items: center; padding: 0.5rem 0; border-bottom: 1px solid var(--line); }
.list label { display: flex; gap: 0.6rem; align-items: center; }
.done span { text-decoration: line-through; color: var(--muted); }
.count { color: var(--muted); font-size: 0.9rem; }
`,
    },
  },

  {
    id: "css-signals",
    title: "Zero-JS with CSS signals",
    description: "createCSSChoice, createCSSGroup and createCSSToggle compile to pure CSS. The output ships no JavaScript for the widgets.",
    tags: ["css signals", "zero-js"],
    entry: "/",
    open: "src/pages/index.tsx",
    files: {
      "src/pages/index.tsx": `import { createCSSChoice, createCSSGroup, createCSSToggle } from "@krate/runtime";
import "../styles.css";

export default function Home() {
  // Each scope is compiled to :has() / :checked CSS. Check the JS tab: there
  // is no script for these widgets.
  const tab = createCSSChoice("a", { as: "tabs" });
  const faq = createCSSGroup(null, { as: "accordion" });
  const note = createCSSToggle(true);

  return (
    <main>
      <h1>Zero-JS widgets</h1>

      <div role="tablist" class="tabs">
        <button {...tab.trigger("a")}>Overview</button>
        <button {...tab.trigger("b")}>Details</button>
        <button {...tab.trigger("c")}>Notes</button>
      </div>
      <div {...tab.panel("a")} class="panel">Overview panel. Switch tabs without any JavaScript.</div>
      <div {...tab.panel("b")} class="panel">Details panel.</div>
      <div {...tab.panel("c")} class="panel">Notes panel.</div>

      <h2>Accordion</h2>
      <button {...faq.trigger("q1")} class="q">What is a CSS signal?</button>
      <div {...faq.panel("q1")} class="panel">State kept in CSS selectors instead of JS variables.</div>
      <button {...faq.trigger("q2")} class="q">Does it need JavaScript?</button>
      <div {...faq.panel("q2")} class="panel">No. The compiler erases it.</div>

      <div {...note.panel(true)} class="note">
        Dismissable note. <button {...note.close()}>Dismiss</button>
      </div>
    </main>
  );
}
`,
      "src/styles.css":
        base +
        `h2 { font-size: 1.1rem; margin: 1.5rem 0 0.5rem; }
.tabs { display: flex; gap: 0.25rem; }
.tabs [aria-selected="true"] { border-color: var(--accent); color: var(--accent); }
.panel { padding: 0.75rem 1rem; border: 1px solid var(--line); border-radius: 8px; margin: 0.5rem 0; }
.q { display: block; width: 100%; text-align: left; }
.note { margin-top: 1.5rem; padding: 0.75rem 1rem; background: var(--soft); border-radius: 8px; }
`,
    },
  },

  {
    id: "components",
    title: "Components",
    description: "@krate/components: Tabs, Accordion, Dialog, Switch, Slider and Badge.",
    tags: ["components"],
    entry: "/",
    open: "src/pages/index.tsx",
    files: {
      "src/pages/index.tsx": `import { createSignal } from "@krate/runtime";
import { Accordion, Badge, Dialog, Slider, Switch, Tabs } from "@krate/components";
import "../styles.css";

export default function Home() {
  const [volume, setVolume] = createSignal(40);

  return (
    <main>
      <h1>
        Components <Badge>@krate/components</Badge>
      </h1>

      <Tabs defaultValue="one">
        <Tabs.List>
          <Tabs.Trigger value="one">Account</Tabs.Trigger>
          <Tabs.Trigger value="two">Password</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="one">Make changes to your account.</Tabs.Content>
        <Tabs.Content value="two">Change your password here.</Tabs.Content>
      </Tabs>

      <section>
        <Switch label="Notifications" defaultChecked />
      </section>

      <section>
        <Slider label="Volume" min={0} max={100} defaultValue={40} onInput={(e: Event) => setVolume(Number((e.target as HTMLInputElement).value))} />
        <p>Volume: {volume()}</p>
      </section>

      <Accordion defaultValue="q1">
        <Accordion.Item>
          <Accordion.Trigger value="q1">Is it accessible?</Accordion.Trigger>
          <Accordion.Content value="q1">Yes. It follows the WAI-ARIA patterns.</Accordion.Content>
        </Accordion.Item>
        <Accordion.Item>
          <Accordion.Trigger value="q2">Does it ship JavaScript?</Accordion.Trigger>
          <Accordion.Content value="q2">Mostly CSS signals, so very little.</Accordion.Content>
        </Accordion.Item>
      </Accordion>

      <Dialog label="Example dialog">
        <Dialog.Trigger class="open-dialog">Open dialog</Dialog.Trigger>
        <Dialog.Content>
          <Dialog.Title>Hello</Dialog.Title>
          <Dialog.Description>This dialog is pure CSS plus a small keyboard enhancer.</Dialog.Description>
          <Dialog.Close>Close</Dialog.Close>
        </Dialog.Content>
      </Dialog>
    </main>
  );
}
`,
      "src/styles.css":
        base +
        `section { margin: 1.25rem 0; }
.open-dialog { display: inline-block; margin-top: 1.25rem; padding: 0.4rem 0.9rem; border-radius: 8px; border: 1px solid var(--line); background: var(--soft); cursor: pointer; }
.open-dialog:hover { border-color: var(--accent); }
`,
    },
  },

  {
    id: "tailwind",
    title: "Tailwind CSS",
    description: "Utility classes with the built-in Go Tailwind: just @import \"tailwindcss\".",
    tags: ["styling", "tailwind"],
    entry: "/",
    open: "src/pages/index.tsx",
    files: {
      "src/pages/index.tsx": `import { createSignal } from "@krate/runtime";
import "../styles.css";

export default function Home() {
  const [liked, setLiked] = createSignal(false);

  return (
    <main class="min-h-screen bg-slate-100 p-6 dark:bg-slate-900">
      <div class="mx-auto max-w-sm rounded-2xl bg-white p-6 shadow-lg dark:bg-slate-800">
        <h1 class="text-2xl font-bold text-slate-900 dark:text-white">Tailwind</h1>
        <p class="mt-2 text-slate-600 dark:text-slate-300">
          Utility classes are compiled by Krate itself, with no PostCSS or Node.
        </p>
        <button
          type="button"
          onClick={() => setLiked(!liked())}
          class={
            "mt-4 rounded-lg px-4 py-2 font-medium text-white transition " +
            (liked() ? "bg-rose-600 hover:bg-rose-700" : "bg-indigo-600 hover:bg-indigo-700")
          }
        >
          {liked() ? "Liked" : "Like"}
        </button>
      </div>
    </main>
  );
}
`,
      "src/styles.css": `@import "tailwindcss";
`,
    },
  },

  {
    id: "css-modules",
    title: "CSS Modules",
    description: "Scoped class names with *.module.css.",
    tags: ["styling"],
    entry: "/",
    open: "src/pages/index.tsx",
    files: {
      "src/pages/index.tsx": `import styles from "../components/card.module.css";
import "../styles.css";

export default function Home() {
  return (
    <main>
      <h1>CSS Modules</h1>
      <div class={styles.card}>
        <h2 class={styles.title}>Scoped styles</h2>
        <p>The class names below are rewritten at build time, so they cannot leak.</p>
        <span class={styles.tag}>.card .title .tag</span>
      </div>
    </main>
  );
}
`,
      "src/components/card.module.css": `.card { padding: 1rem 1.25rem; border: 1px solid var(--line); border-radius: 12px; background: var(--soft); }
.title { margin: 0 0 0.5rem; font-size: 1.1rem; color: var(--accent); }
.tag { display: inline-block; padding: 0.1rem 0.5rem; border-radius: 999px; background: var(--bg); border: 1px solid var(--line); font: 0.8rem ui-monospace, monospace; }
`,
      "src/styles.css": base,
    },
  },

  {
    id: "multi-page",
    title: "Multi-page site",
    description: "File-based routing with a shared layout and Link navigation.",
    tags: ["routing"],
    entry: "/",
    open: "src/pages/index.tsx",
    files: {
      "src/pages/_layout.tsx": `import "../styles.css";

export default function Layout(props: { children: any }) {
  return (
    <div class="shell">
      <nav aria-label="Main">
        <Link href="/">Home</Link>
        <Link href="/about/">About</Link>
        <Link href="/contact/">Contact</Link>
      </nav>
      <main>{props.children}</main>
    </div>
  );
}
`,
      "src/pages/index.tsx": `export default function Home() {
  return (
    <>
      <h1>Home</h1>
      <p>Each file under src/pages becomes a route. Click a link above.</p>
    </>
  );
}
`,
      "src/pages/about.tsx": `export default function About() {
  return (
    <>
      <h1>About</h1>
      <p>This page lives at <code>src/pages/about.tsx</code>.</p>
    </>
  );
}
`,
      "src/pages/contact.tsx": `export default function Contact() {
  return (
    <>
      <h1>Contact</h1>
      <p>Add a file, and a new route appears.</p>
    </>
  );
}
`,
      "src/styles.css":
        base +
        `nav { display: flex; gap: 1rem; padding: 0.75rem 1.25rem; border-bottom: 1px solid var(--line); }
body { padding: 0; }
main { padding: 1.5rem 1.25rem; }
code { background: var(--soft); padding: 0.1rem 0.35rem; border-radius: 4px; }
`,
    },
  },

  {
    id: "markdown",
    title: "Markdown page",
    description: "A Markdown file with frontmatter becomes a page.",
    tags: ["content"],
    entry: "/notes/",
    open: "src/pages/notes.md",
    files: {
      "src/pages/index.tsx": `import "../styles.css";

export default function Home() {
  return (
    <main>
      <h1>Home</h1>
      <p><Link href="/notes/">Read the notes</Link></p>
    </main>
  );
}
`,
      "src/pages/notes.md": `---
title: Notes
description: A page written in Markdown.
---

# Notes

Markdown pages support **bold**, *italic*, \`inline code\`, and [links](/).

## A list

- Frontmatter sets the title
- GitHub-flavored tables and task lists work
- [x] Done
- [ ] Not yet

| Feature | Supported |
| ------- | --------- |
| Tables  | yes       |
| Headings with anchors | yes |

\`\`\`ts
const answer: number = 42;
\`\`\`
`,
      "src/styles.css": base,
    },
  },

  {
    id: "fetch",
    title: "Fetch with createResource",
    description: "Load JSON on the client; shows a friendly message when offline.",
    tags: ["data"],
    entry: "/",
    open: "src/pages/index.tsx",
    files: {
      "src/pages/index.tsx": `import { createSignal, createResource } from "@krate/runtime";
import "../styles.css";

export default function Home() {
  const [id, setId] = createSignal(1);

  // Refetches whenever id() changes; in-flight requests are aborted.
  const [post, { refetch }] = createResource(() => id(), async (n) => {
    const res = await fetch("https://jsonplaceholder.typicode.com/posts/" + n);
    if (!res.ok) throw new Error("HTTP " + res.status);
    return (await res.json()) as { title: string; body: string };
  });

  return (
    <main>
      <h1>Post #{id()}</h1>
      {post.loading && <p class="muted">Loading...</p>}
      {post.error && (
        <p role="alert" class="error">Could not load the post (are you offline?).</p>
      )}
      {post() && (
        <article>
          <h2>{post().title}</h2>
          <p>{post().body}</p>
        </article>
      )}
      <div class="row">
        <button type="button" onClick={() => setId(Math.max(1, id() - 1))}>Previous</button>
        <button type="button" onClick={() => setId(id() + 1)}>Next</button>
        <button type="button" onClick={refetch}>Refetch</button>
      </div>
    </main>
  );
}
`,
      "src/styles.css":
        base +
        `.row { display: flex; gap: 0.5rem; margin-top: 1rem; }
.muted { color: var(--muted); }
.error { color: #dc2626; }
h2 { font-size: 1.1rem; text-transform: capitalize; }
`,
    },
  },

  {
    id: "stopwatch",
    title: "Effects and timers",
    description: "createEffect with onCleanup to run an interval.",
    tags: ["signals", "effects"],
    entry: "/",
    open: "src/pages/index.tsx",
    files: {
      "src/pages/index.tsx": `import { createSignal, createEffect, onCleanup } from "@krate/runtime";
import "../styles.css";

export default function Home() {
  const [running, setRunning] = createSignal(false);
  const [ms, setMs] = createSignal(0);

  // The effect re-runs when running() changes; onCleanup stops the old timer.
  createEffect(() => {
    if (!running()) return;
    const start = performance.now() - ms();
    const timer = setInterval(() => setMs(performance.now() - start), 33);
    onCleanup(() => clearInterval(timer));
  });

  const text = () => {
    const total = Math.floor(ms() / 10);
    const cs = String(total % 100).padStart(2, "0");
    const s = String(Math.floor(total / 100) % 60).padStart(2, "0");
    const m = String(Math.floor(total / 6000)).padStart(2, "0");
    return m + ":" + s + "." + cs;
  };

  return (
    <main>
      <h1>Stopwatch</h1>
      <p class="time" aria-live="off">{text()}</p>
      <div class="row">
        <button type="button" onClick={() => setRunning(!running())}>{running() ? "Stop" : "Start"}</button>
        <button type="button" onClick={() => { setRunning(false); setMs(0); }}>Reset</button>
      </div>
    </main>
  );
}
`,
      "src/styles.css":
        base +
        `.time { font: 700 3.5rem ui-monospace, monospace; margin: 0.5rem 0 1rem; font-variant-numeric: tabular-nums; }
.row { display: flex; gap: 0.5rem; }
`,
    },
  },
];

export const defaultExampleId = "counter";
