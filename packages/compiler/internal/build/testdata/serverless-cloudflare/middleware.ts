import { getPlatform } from "@krate/runtime/server";

const edge: Record<string, Record<string, string>> = {
  "/edge-public": { "cache-control": "public, s-maxage=60" },
  "/edge-nostore": { "cache-control": "no-store" },
  "/edge-cookie": { "cache-control": "public, s-maxage=60", "set-cookie": "a=1; Path=/" },
};

export function middleware(request: Request) {
  const headers: Record<string, string> = { "x-middleware-next": "1", "x-mw-db": String(getPlatform<any>().env.DB) };
  Object.assign(headers, edge[new URL(request.url).pathname.replace(/\/$/, "")]);
  return new Response(null, { headers });
}
export const config = { matcher: ["/platform", "/api/:path*", "/edge-public", "/edge-nostore", "/edge-cookie"] };
