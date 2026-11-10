export async function GET() {
  const out: Record<string, unknown> = {};
  for (const s of ["Zg=", "Z===", "Zm9v=", "Zg=a"]) {
    try { out[s] = Array.from(atob(s), (c) => c.charCodeAt(0)); } catch (e) { out[s] = e.name; }
  }
  return Response.json(out);
}
