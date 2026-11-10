const inputs = ["HTTP://EXAMPLE.COM/A/../b", "http:\\\\x.example\\y\\z", "ht\ntp://x.example/a\tb", "  http://x.example/  ", "http://x.example/./a/../b/./c", "http://x.example/%2e%2e/a", "http://x.example/a/%2E/b", "http://x.example", "http://x.example?q", "http://x.example#f", "http:/x.example/p", "http:x.example/p", "HTTPS://x.example:443", "file:/a/b", "file:///C:/x/../y"];
export async function GET() {
  const out: Record<string, string> = {};
  for (const i of inputs) { try { out[JSON.stringify(i)] = new URL(i).href; } catch (e) { out[JSON.stringify(i)] = "invalid"; } }
  return Response.json(out);
}
