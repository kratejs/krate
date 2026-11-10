export async function GET() {
  const u = new URL("https://us%40er:pa%20ss@host.example/p");
  const only = new URL("http://user@h.example/");
  const empty = new URL("http://:@h.example/");
  const raw = new URL("http://a b:c d@h.example/");
  return Response.json({ parsed: [u.username, u.password, u.href], only: [only.username, only.password, only.href], empty: empty.href, raw: [raw.username, raw.password] });
}
