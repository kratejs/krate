export async function GET() {
  const h = new Headers();
  h.append("Set-Cookie", "a=1; Path=/; HttpOnly");
  h.append("Set-Cookie", "b=2; Path=/");
  h.set("X-Custom", "v");
  return new Response("ok", { headers: h });
}
