export async function GET() {
  const u = new URL("http://example.com/a b/ü/%7Efoo/a%2fb/[x]?q=a b&ü=1&x='\"<>`#frag ment<>");
  const v = new URL("http://example.com/?a=%zz&b=%E2%82%AC");
  const s = new URL("http://example.com/{}|/");
  return Response.json({
    href: u.href, pathname: u.pathname, search: u.search, hash: u.hash,
    q: u.searchParams.get("q"), u: u.searchParams.get("ü"),
    raw: v.search, a: v.searchParams.get("a"), b: v.searchParams.get("b"),
    special: s.pathname,
    nonspecial: new URL("foo://h/a b?c d#e f").href,
  });
}
