export async function GET() {
  const out: Record<string, unknown> = {};
  for (const s of ["", "Zg==", "Zg", "Zm8=", "Zm8", "Zm9v", " Zm 9v\n", "Zm9v\t\r\f", "Zh==", "Zm9=", "/+/+", "AAAA"]) {
    try { out[JSON.stringify(s)] = Array.from(atob(s), (c) => c.charCodeAt(0)); } catch (e) { out[JSON.stringify(s)] = "throws:" + e.name; }
  }
  return Response.json(out);
}
