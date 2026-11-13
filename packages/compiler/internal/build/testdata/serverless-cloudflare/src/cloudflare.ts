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
  // Krate's own fetch handler stays; this one is ignored.
  async fetch() {
    return new Response("not krate");
  },
};
