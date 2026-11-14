import { getPlatform } from "@krate/runtime/server";

export function middleware() {
  return new Response(null, { headers: { "x-middleware-next": "1", "x-mw-db": String(getPlatform<any>().env.DB) } });
}
export const config = { matcher: ["/platform", "/api/:path*"] };
