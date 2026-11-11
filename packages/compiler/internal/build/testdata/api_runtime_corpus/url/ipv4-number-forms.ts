const inputs = ["http://0x7f.1/", "http://2130706433/", "http://127.1/", "http://0177.0.0.1/", "http://192.168.0.257/", "http://1.2.3.4.5/", "http://127.0.0.1/"];
export async function GET() {
  const out: Record<string, unknown> = {};
  for (const i of inputs) {
    try { const u = new URL(i); out[i] = [u.href, u.host, u.hostname, u.port]; } catch (e) { out[i] = "invalid:" + e.name; }
  }
  return Response.json(out);
}
