export async function GET() {
  const out: Record<string, unknown> = {};
  for (const s of ["€", "Ā", "😀"]) {
    try { btoa(s); out[s] = "ok"; } catch (e) { out[s] = e.name + ":" + (e instanceof DOMException) + ":" + e.code; }
  }
  return Response.json(out);
}
