export function deferred<T = void>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

export const tick = () => new Promise<void>((r) => setTimeout(r, 0));
export async function flushAll() {
  for (let i = 0; i < 5; i++) await tick();
}
