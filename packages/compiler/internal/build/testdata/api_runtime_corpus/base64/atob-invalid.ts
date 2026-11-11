export async function GET() {
  const out: Record<string, unknown> = {};
  for (const s of ["*", "a", "abcde", "Zg===", "Zg==Zg==", "=Zg=", "Zm9v=", "-_-_", "Zmév"]) {
    try { atob(s); out[JSON.stringify(s)] = "ok"; } catch (e) { out[JSON.stringify(s)] = e.name + ":" + (e instanceof DOMException); }
  }
  return Response.json(out);
}
