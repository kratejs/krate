// Timers are compared by order only. Their delays are far apart so that a slow
// machine (the race detector, a loaded CI runner) cannot reorder them: the
// longer timer is registered first, and registration itself can take
// milliseconds there.
export async function GET() {
  const log: string[] = [];
  setTimeout(() => log.push("timeoutLong"), 200);
  setTimeout(() => log.push("timeout0"), 0);
  setTimeout(() => log.push("timeout0b"));
  setTimeout(() => log.push("timeoutShort"), 50);
  Promise.resolve().then(() => log.push("promise1"));
  queueMicrotask(() => log.push("microtask"));
  Promise.resolve().then(() => log.push("promise2"));
  (async () => { log.push("async-sync"); await null; log.push("async-after-await"); })();
  log.push("sync");
  await new Promise((r) => setTimeout(r, 300));
  return Response.json(log);
}
