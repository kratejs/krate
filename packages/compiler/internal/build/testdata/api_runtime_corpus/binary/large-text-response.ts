export async function GET() {
  return new Response("é\ud83d\ude00abc".repeat(100000), { headers: { "content-type": "text/plain; charset=utf-8" } });
}
