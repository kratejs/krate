export function middleware(request: Request) {
  const url = new URL(request.url);
  if (url.pathname === "/admin") return Response.redirect(new URL("/about", request.url), 307);
  if (url.pathname === "/teapot") return new Response("teapot", { status: 418 });
  return new Response(null, { headers: { "x-middleware-next": "1", "x-mw": "ran" } });
}
export const config = { matcher: ["/admin", "/teapot", "/api/:path*", "/ssr", "/"] };
