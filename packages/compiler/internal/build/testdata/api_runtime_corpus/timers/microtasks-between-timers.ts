export async function GET() {
  const log: string[] = [];
  setTimeout(() => { log.push("t1"); Promise.resolve().then(() => log.push("t1-micro")); queueMicrotask(() => log.push("t1-qm")); }, 0);
  setTimeout(() => log.push("t2"), 0);
  await new Promise((r) => setTimeout(r, 20));
  return Response.json(log);
}
