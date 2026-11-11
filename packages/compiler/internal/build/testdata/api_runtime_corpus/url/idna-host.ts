const inputs = ["http://EXAMPLE.com/", "http://münchen.de/", "http://xn--mnchen-3ya.de/", "http://ÉXAMPLE.com/", "http://example.com./", "http://☃.net/"];
export async function GET() {
  const out: Record<string, unknown> = {};
  for (const i of inputs) { try { const u = new URL(i); out[i] = [u.host, u.href]; } catch (e) { out[i] = "invalid:" + e.name; } }
  return Response.json(out);
}
