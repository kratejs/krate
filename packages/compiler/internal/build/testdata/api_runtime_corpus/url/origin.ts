const inputs = ["http://x.example/", "http://x.example:80/", "https://x.example:443/", "https://x.example:8443/a", "HTTP://X.EXAMPLE:8080", "ws://x.example:80/", "wss://x.example/", "ftp://x.example:21/", "file:///etc/passwd", "data:text/plain,hi", "about:blank", "foo://bar/baz", "javascript:alert(1)"];
export async function GET() {
  const out: Record<string, unknown> = {};
  for (const i of inputs) { const u = new URL(i); out[i] = [u.origin, u.protocol, u.port, u.host]; }
  return Response.json(out);
}
