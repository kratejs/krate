// What the compiler generates for `import { List, Add } from "../lib/tasks.go"`
// (and, with ids like "src/lib/notes.host.greet", for a TypeScript host module).
import { call } from "../../src/index.js";

export const List = (opts?: unknown) => call<{ id: number; title: string }[]>("src/lib/tasks.List", [], opts as never);
export const Add = (title: string, opts?: unknown) => call<{ id: number; title: string }>("src/lib/tasks.Add", [title], opts as never);
export const greet = (name: string) => call<string>("src/lib/notes.host.greet", [name]);
