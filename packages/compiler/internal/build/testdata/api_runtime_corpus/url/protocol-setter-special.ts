export async function GET() {
  const u = new URL("https://h.example/a");
  u.protocol = "foo"; const a = u.href;
  const v = new URL("foo://h.example/a");
  v.protocol = "http"; const b = v.href;
  return Response.json({ a, b });
}
