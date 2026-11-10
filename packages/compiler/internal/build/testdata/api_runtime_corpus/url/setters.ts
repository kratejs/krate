export async function GET() {
  const u = new URL("http://h.example:81/a/b?q=1#f");
  const log: string[] = [];
  u.protocol = "https"; log.push(u.href);
  u.port = "443"; log.push(u.href);
  u.port = "8080"; log.push(u.href);
  u.port = "abc"; log.push(u.href);
  u.port = ""; log.push(u.href);
  u.hostname = "other.example"; log.push(u.href);
  u.host = "third.example:9000"; log.push(u.href);
  u.pathname = "x y/z"; log.push(u.href);
  u.hash = "new hash"; log.push(u.href);
  u.hash = ""; log.push(u.href);
  u.search = "a=b c"; log.push(u.href);
  return Response.json(log);
}
