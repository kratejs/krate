const inputs = ["http://[::1]:8080/x", "http://[2001:db8::1]/", "http://[::]/", "http://[1:2:3:4:5:6:7:8]/", "http://[::1/", "http://[::1]x/"];
export async function GET() {
  const out: Record<string, unknown> = {};
  for (const i of inputs) {
    try { const u = new URL(i); out[i] = [u.href, u.host, u.hostname, u.port]; } catch (e) { out[i] = "invalid:" + e.name; }
  }
  return Response.json(out);
}
