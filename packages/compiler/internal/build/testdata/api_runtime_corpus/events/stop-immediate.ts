export async function GET() {
  const t = new EventTarget();
  const log: string[] = [];
  t.addEventListener("x", (e) => { log.push("1"); e.stopImmediatePropagation(); });
  t.addEventListener("x", () => log.push("2"));
  t.dispatchEvent(new Event("x"));
  return Response.json(log);
}
