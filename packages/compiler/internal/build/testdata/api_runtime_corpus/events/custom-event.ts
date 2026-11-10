export async function GET() {
  if (typeof CustomEvent !== "function") return Response.json({ missing: true });
  const t = new EventTarget();
  let detail: unknown = null;
  t.addEventListener("c", (e: any) => { detail = e.detail; });
  t.dispatchEvent(new CustomEvent("c", { detail: { a: 1 } }));
  return Response.json({ detail });
}
