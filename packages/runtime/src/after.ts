/**
 * `after(task)` mirrors `next/server`'s `after()`: it schedules work that runs
 * once the response has been sent (logging, analytics, cache warming). Inside
 * an API route the task is queued on the request and the server runs it after
 * the response is flushed, in registration order. Errors are logged, never
 * sent to the client. Where no request is active (the embedded runtime, tests)
 * the task runs on a later microtask turn, right after the handler settles.
 */
import { currentRevalidateScope } from './cache-scope.js';

export type AfterTask = (() => unknown) | PromiseLike<unknown>;

function toThunk(task: AfterTask): () => unknown {
  if (typeof task === 'function') return task;
  if (task && typeof (task as PromiseLike<unknown>).then === 'function') return () => task;
  throw new TypeError('after() expects a function or a promise');
}

export function after(task: AfterTask): void {
  const thunk = toThunk(task);
  const scope = currentRevalidateScope();
  if (scope) {
    (scope.after ||= []).push(thunk);
    return;
  }
  void Promise.resolve()
    .then(thunk)
    .catch((err) => console.error(`[krate] after() task failed: ${(err && (err as Error).stack) || err}`));
}

/** Runs and clears a scope's after() tasks in order; each failure is logged. */
export async function runAfterTasks(scope: { after?: Array<() => unknown> }): Promise<void> {
  const tasks = scope.after;
  if (!tasks || tasks.length === 0) return;
  scope.after = [];
  for (const t of tasks) {
    try {
      await t();
    } catch (err) {
      console.error(`[krate] after() task failed: ${(err && (err as Error).stack) || err}`);
    }
  }
}

/** request.cookies on an API route's Request, shaped like Next's RequestCookies (read-only). */
export interface KrateRequestCookies {
  get(name: string): { name: string; value: string } | undefined;
  getAll(name?: string): Array<{ name: string; value: string }>;
  has(name: string): boolean;
}

/** request.nextUrl: the request URL plus Next's basePath and clone(). */
export interface KrateNextURL extends URL {
  readonly basePath: string;
  clone(): KrateNextURL;
}

/** The Request an API route handler receives: a web Request with Next-style cookies and nextUrl. */
export type KrateRequest = Request & {
  readonly cookies: KrateRequestCookies;
  readonly nextUrl: KrateNextURL;
};
