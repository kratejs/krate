import { env } from "cloudflare:workers";
import { getPlatform } from "@krate/runtime/server";

export async function GET() {
  return Response.json({ db: (env as any).DB, kv: getPlatform<any>().env.KV });
}
