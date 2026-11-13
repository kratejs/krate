// @vitest-environment node
import { describe, expect, it } from "vitest";
import { cookies, headers, redirect } from "./server.js";
import { callRoute, runAction } from "./testing.js";

describe("callRoute", () => {
  const route = {
    async GET(req: Request, ctx: { params: Record<string, string> }) {
      const url = new URL(req.url);
      return Response.json({ id: ctx.params.id, q: url.searchParams.get("q") });
    },
    async POST(req: Request) {
      return Response.json(await req.json(), { status: 201 });
    },
  };

  it("passes the request and route params to a named export", async () => {
    const res = await callRoute(route, "GET", "/api/users/7?q=x", { params: { id: "7" } });
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ id: "7", q: "x" });
  });

  it("sends a JSON body", async () => {
    const res = await callRoute(route, "POST", "/api/users", { json: { name: "ada" } });
    expect(res.status).toBe(201);
    expect(await res.json()).toEqual({ name: "ada" });
  });

  it("answers HEAD from GET, OPTIONS and 405 like the server", async () => {
    const head = await callRoute(route, "HEAD", "/api/users/7");
    expect(head.status).toBe(200);
    expect(await head.text()).toBe("");
    const options = await callRoute(route, "OPTIONS", "/api/users");
    expect(options.headers.get("allow")).toBe("GET, POST, HEAD, OPTIONS");
    const missing = await callRoute(route, "DELETE", "/api/users");
    expect(missing.status).toBe(405);
    expect(missing.headers.get("allow")).toContain("GET");
  });

  it("calls a default Web handler for every method and rejects the legacy signature", async () => {
    const any = { default: async (req: Request) => new Response(req.method) };
    expect(await (await callRoute(any, "PUT", "/x")).text()).toBe("PUT");
    const legacy = { default: (_req: unknown, res: { json(v: unknown): void }) => res.json({}) };
    await expect(callRoute(legacy as never, "GET", "/x")).rejects.toThrow(/legacy/);
  });
});

describe("runAction", () => {
  it("gives the action cookies() and headers() and records cookie writes", async () => {
    async function login() {
      const jar = cookies();
      const before = jar.get("session");
      jar.set("session", "abc", { httpOnly: true });
      jar.delete("old");
      return { before, agent: headers().get("user-agent") };
    }
    const { result, setCookies } = await runAction(login, {
      cookies: { session: "prev", old: "1" },
      headers: { "user-agent": "vitest" },
    });
    expect(result).toEqual({ before: { name: "session", value: "prev" }, agent: "vitest" });
    expect(setCookies).toEqual([
      { name: "session", value: "abc", options: { httpOnly: true }, deleted: false },
      { name: "old", value: "", options: undefined, deleted: true },
    ]);
  });

  it("rejects with what the action throws, including redirect()", async () => {
    await expect(runAction(async () => redirect("/done"))).rejects.toBeDefined();
    await expect(runAction(async () => { throw new Error("nope"); })).rejects.toThrow("nope");
  });

  it("keeps parallel actions apart", async () => {
    const read = async () => {
      await new Promise((r) => setTimeout(r, 5));
      return cookies().get("who");
    };
    const [a, b] = await Promise.all([runAction(read, { cookies: { who: "a" } }), runAction(read, { cookies: { who: "b" } })]);
    expect([a.result, b.result]).toEqual([{ name: "who", value: "a" }, { name: "who", value: "b" }]);
  });
});
