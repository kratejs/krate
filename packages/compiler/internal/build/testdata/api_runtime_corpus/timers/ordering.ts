export async function GET() {
  const log: string[] = [];
  setTimeout(() => log.push("timeout20"), 20);
  setTimeout(() => log.push("timeout0"), 0);
  setTimeout(() => log.push("timeout0b"));
  setTimeout(() => log.push("timeout5"), 5);
  Promise.resolve().then(() => log.push("promise1"));
  queueMicrotask(() => log.push("microtask"));
  Promise.resolve().then(() => log.push("promise2"));
  (async () => { log.push("async-sync"); await null; log.push("async-after-await"); })();
  log.push("sync");
  await new Promise((r) => setTimeout(r, 40));
  return Response.json(log);
}
