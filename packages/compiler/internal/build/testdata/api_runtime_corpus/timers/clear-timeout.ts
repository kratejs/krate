export async function GET() {
  const log: string[] = [];
  const a = setTimeout(() => log.push("a"), 5);
  clearTimeout(a);
  const b = setTimeout(() => log.push("b"), 100);
  setTimeout(() => { log.push("c"); clearTimeout(b); }, 1);
  clearTimeout(undefined); clearTimeout(null as any); clearTimeout(123456 as any);
  const d = setTimeout(() => log.push("d"), 5);
  clearInterval(d);
  await new Promise((r) => setTimeout(r, 150));
  return Response.json(log);
}
