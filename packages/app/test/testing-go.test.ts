import { afterEach, describe, expect, it } from "vitest";
import { mockHost, resetHostMocks } from "../src/testing.js";
import { List, Add, greet } from "./fixtures/tasks-stub.js";
import { createResource } from "../../runtime/src/resource.js";

afterEach(resetHostMocks);

// A component's logic that uses a Go function, as a page would.
async function openTitles(): Promise<string[]> {
  return (await List()).filter((t) => t.title).map((t) => t.title);
}

describe("mockHost with Go and TypeScript host modules", () => {
  it("matches a .go module by its module id (the path without .go)", async () => {
    mockHost("src/lib/tasks", {
      List: async () => [{ id: 1, title: "mocked" }],
      Add: (title: string) => ({ id: 2, title }),
    });
    expect(await openTitles()).toEqual(["mocked"]);
    expect(await Add("x")).toEqual({ id: 2, title: "x" });
  });

  it("matches a .host.ts module by its id, which keeps the .host", async () => {
    mockHost("src/lib/notes.host", { greet: (n: string) => `hi ${n}` });
    expect(await greet("ann")).toBe("hi ann");
  });

  it("does not leak between tests: unmocked modules use the network", async () => {
    await expect(List()).rejects.toBeDefined();
  });
});

describe("host calls as resources", () => {
  it("createResource(undefined, () => List()) loads and refetches", async () => {
    let n = 0;
    mockHost("src/lib/tasks", { List: async () => [{ id: ++n, title: "t" + n }] });
    const [tasks, { refetch }] = createResource<{ id: number; title: string }[], void>(undefined, () => List());
    expect(tasks.loading).toBe(true);
    await new Promise((r) => setTimeout(r, 10));
    expect(tasks()?.[0].title).toBe("t1");
    expect(tasks.state).toBe("ready");
    refetch();
    await new Promise((r) => setTimeout(r, 10));
    expect(tasks()?.[0].title).toBe("t2");
  });

  it("a Go error reaches the resource", async () => {
    mockHost("src/lib/tasks", {
      List: async () => {
        throw Object.assign(new Error("gone"), { code: "not_found" });
      },
    });
    const [tasks] = createResource<unknown[], void>(undefined, () => List());
    await new Promise((r) => setTimeout(r, 10));
    expect(tasks.state).toBe("error");
    expect((tasks.error as { code: string }).code).toBe("not_found");
  });
});
