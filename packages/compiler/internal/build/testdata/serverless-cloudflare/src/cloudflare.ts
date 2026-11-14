import { DurableObject } from "cloudflare:workers";
import { getPlatform } from "@krate/runtime/server";

export class Counter extends DurableObject {
  value() {
    return 7;
  }
}

export async function scheduled(controller: { cron: string }, _env: unknown, ctx: { waitUntil(p: Promise<unknown>): void }) {
  ctx.waitUntil(Promise.resolve());
  (globalThis as any).__cron = controller.cron + ":" + getPlatform<any>().env.DB;
}

export default {
  async queue(batch: { messages: unknown[] }) {
    (globalThis as any).__queued = batch.messages.length;
  },
  // Wraps Krate's handler; next() renders the request.
  async fetch(request: Request, _env: unknown, _ctx: unknown, next: (r?: Request) => Promise<Response>) {
    const res = await next(request);
    const out = new Response(res.body, res);
    out.headers.set("x-worker-hook", "1");
    return out;
  },
};
