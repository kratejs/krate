export async function GET() {
  const t = new EventTarget();
  const log: string[] = [];
  const fn = (e: Event) => log.push("fn:" + e.type);
  t.addEventListener("x", fn); t.addEventListener("x", fn);
  t.addEventListener("x", { handleEvent(e: Event) { log.push("obj:" + e.type); } });
  t.addEventListener("x", () => log.push("once"), { once: true });
  t.addEventListener("x", null as any);
  t.dispatchEvent(new Event("x"));
  t.dispatchEvent(new Event("x"));
  t.removeEventListener("x", fn);
  t.dispatchEvent(new Event("x"));
  t.dispatchEvent(new Event("other"));
  return Response.json(log);
}
