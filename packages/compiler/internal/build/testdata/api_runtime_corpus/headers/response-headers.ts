export async function GET() {
  return new Response("ok", { headers: [["X-Custom", "one"], ["x-custom", "two"], ["Content-Type", "text/plain; charset=utf-8"]] });
}
