// Promises tagged with their state, so the root can tell synchronously
// whether configuration has settled.

export type TrackedPromise<T> = Promise<T> &
  ({ status: 'pending' } | { status: 'fulfilled'; value: T } | { status: 'rejected'; reason: unknown });

export const isPromiseLike = (value: unknown): value is PromiseLike<unknown> =>
  typeof (value as PromiseLike<unknown> | undefined)?.then === 'function';

export const fulfilled = <T>(value: T): TrackedPromise<T> =>
  Object.assign(Promise.resolve(value), { status: 'fulfilled' as const, value });

export const rejected = <T = never>(reason: unknown): TrackedPromise<T> => {
  const promise = Promise.reject(reason);
  promise.catch(() => {});
  return Object.assign(promise, { status: 'rejected' as const, reason }) as TrackedPromise<T>;
};

/** A pending promise settled from outside; its `status` updates when it settles. */
export function deferred<T>(): { promise: TrackedPromise<T>; resolve: (v: T) => void; reject: (e: unknown) => void } {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = (value) => {
      Object.assign(promise, { status: 'fulfilled', value });
      yes(value);
    };
    reject = (reason) => {
      Object.assign(promise, { status: 'rejected', reason });
      no(reason);
    };
  }) as TrackedPromise<T>;
  (promise as { status: string }).status = 'pending';
  promise.catch(() => {});
  return { promise, resolve, reject };
}
