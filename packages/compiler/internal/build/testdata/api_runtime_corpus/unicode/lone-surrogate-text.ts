export async function GET() {
  return new Response("a\ud800b\udc00c", { headers: { "content-type": "text/plain; charset=utf-8" } });
}
