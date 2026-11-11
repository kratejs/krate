export async function GET() {
  const u = new URL("https://us%40er:pa%20ss@host.example/p");
  u.username = "a b@c"; const a = u.href;
  u.password = "p:w/d"; const b = u.href;
  u.password = ""; const c = u.href;
  u.username = ""; const d = u.href;
  return Response.json({ a, b, c, d });
}
