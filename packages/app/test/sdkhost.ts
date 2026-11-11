// Shared setup for the SDK wrapper tests (data and UI): a fake host whose
// functions record their calls, installed as the shared client.
import { afterEach } from "vitest";
import { createClient, setClient, type Client } from "../src/index.js";
import { createFakeHost, type FakeHandler, type FakeHost } from "../src/fakehost.js";

export interface SeenCall {
  id: string;
  /** Same as id; the UI tests were written against this name. */
  fn: string;
  args: unknown[];
}

let client: Client | undefined;

afterEach(() => {
  client?.close();
  client = undefined;
});

// installFakeHost registers handlers on a fake host, records every call and
// makes the host's client the shared one. A previous install is closed.
export function installFakeHost(
  handlers: Record<string, FakeHandler> = {},
  opts: { windowId?: string } = {},
): { host: FakeHost; seen: SeenCall[] } {
  client?.close();
  const seen: SeenCall[] = [];
  const registry: Record<string, FakeHandler> = {};
  for (const [id, h] of Object.entries(handlers)) {
    registry[id] = (args, ctx) => {
      const call = { id, args } as SeenCall;
      Object.defineProperty(call, "fn", { value: id, enumerable: false });
      seen.push(call);
      return h(args, ctx);
    };
  }
  const host = createFakeHost(registry);
  client = createClient({ fetch: host.fetch, ...(opts.windowId ? { windowId: opts.windowId } : {}) });
  setClient(client);
  return { host, seen };
}
