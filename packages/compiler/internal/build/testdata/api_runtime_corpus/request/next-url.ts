export async function GET(request) {
  const u = request.nextUrl;
  const c = u.clone();
  c.searchParams.set("extra", "1");
  return Response.json({
    pathname: u.pathname,
    code: u.searchParams.get("code"),
    basePath: u.basePath,
    clone: c.search,
    original: u.search,
  });
}
