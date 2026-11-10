const inputs = ["http://[2001:DB8:0:0:0:0:0:1]/", "http://[0:0:0:0:0:0:0:1]/", "http://[::ffff:192.168.0.1]/", "http://[1:0:0:2:0:0:0:3]/", "http://[1:2:3:4:5:6:7:8:9]/", "http://[:::1]/", "http://[1::2::3]/"];
export async function GET() {
  const out: Record<string, unknown> = {};
  for (const i of inputs) {
    try { const u = new URL(i); out[i] = [u.href, u.host, u.hostname, u.port]; } catch (e) { out[i] = "invalid:" + e.name; }
  }
  return Response.json(out);
}
