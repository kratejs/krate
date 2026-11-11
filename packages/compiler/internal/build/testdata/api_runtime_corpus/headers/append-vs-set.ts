export async function GET() {
  const h = new Headers();
  h.append("a", "1"); h.append("a", "2"); const appended = h.get("a");
  h.set("a", "3"); const set = h.get("a");
  h.append("a", ""); const emptyAppend = h.get("a");
  h.set("b", "  padded \t"); const trimmed = h.get("b");
  h.set("n", 5 as any); const num = h.get("n");
  return Response.json({ appended, set, emptyAppend, trimmed: JSON.stringify(trimmed), num });
}
