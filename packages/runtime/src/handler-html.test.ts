// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  allSuspense,
  canStreamOutOfOrder,
  flashMessages,
  injectFlashNotice,
  parseRegionShell,
  regionReplaceScript,
  replaceTitle,
  requestFlash,
  signFlash,
  verifyFlash,
  flashPayloadOf,
  splitBodyScripts,
  spliceShell,
} from "./handler-html.js";

const suspense = (id: string, inner = "") => `<!--suspense:${id}-->${inner}<!--/suspense:${id}-->`;
const region = (id: string, inner = "") => `<!--region:${id}-->${inner}<!--/region:${id}-->`;

function splice(shell: string, html: Record<string, string>): string {
  const parsed = parseRegionShell(shell);
  expect(parsed.ok).toBe(true);
  const out: string[] = [];
  spliceShell(shell, parsed.roots, new Map(Object.entries(html)), (c) => out.push(c));
  return out.join("");
}

describe("parseRegionShell", () => {
  it("reports a shell without markers as not ok", () => {
    const p = parseRegionShell("<html><body>static</body></html>");
    expect(p.ok).toBe(false);
    expect(p.roots).toEqual([]);
  });

  it("finds sibling and nested markers in document order", () => {
    const shell = `<main>${suspense("a", "fb-a")}${region("b", suspense("c", "fb-c"))}</main>`;
    const p = parseRegionShell(shell);
    expect(p.ok).toBe(true);
    expect(p.all.map((b) => b.id)).toEqual(["a", "b", "c"]);
    expect(p.roots.map((b) => b.id)).toEqual(["a", "b"]);
    expect(p.roots[1].children.map((b) => b.id)).toEqual(["c"]);
    const a = p.all[0];
    expect(shell.slice(a.openEnd, a.fbEnd)).toBe("fb-a");
  });

  it("rejects a marker whose closing comment is missing, dropping partial results", () => {
    const p = parseRegionShell(`${suspense("a", "x")}<!--suspense:b-->never closed`);
    expect(p.ok).toBe(false);
    expect(p.roots).toEqual([]);
    expect(p.all).toEqual([]);
  });

  it("does not confuse a region and a suspense marker that share an id", () => {
    const p = parseRegionShell(`<!--suspense:x-->s<!--/region:x-->`);
    expect(p.ok).toBe(false);
  });
});

describe("streaming mode", () => {
  it("streams siblings out of order, but not the coarse page region or nested markers", () => {
    expect(canStreamOutOfOrder(parseRegionShell(suspense("a") + suspense("b")).all)).toBe(true);
    expect(canStreamOutOfOrder(parseRegionShell(suspense("page")).all)).toBe(false);
    expect(canStreamOutOfOrder(parseRegionShell(suspense("a", suspense("b"))).all)).toBe(false);
    expect(canStreamOutOfOrder([])).toBe(false);
  });

  it("allSuspense is false as soon as one region marker is present", () => {
    expect(allSuspense(parseRegionShell(suspense("a") + suspense("b")).all)).toBe(true);
    expect(allSuspense(parseRegionShell(suspense("a") + region("b")).all)).toBe(false);
  });
});

describe("splitBodyScripts", () => {
  it("defers the first body script and everything after it", () => {
    const [head, tail] = splitBodyScripts("<html><body><p>x</p><SCRIPT src=/a.js></SCRIPT></body></html>");
    expect(head).toBe("<html><body><p>x</p>");
    expect(tail).toBe("<SCRIPT src=/a.js></SCRIPT></body></html>");
  });

  it("ignores a script in <head>", () => {
    const shell = "<html><head><script>1</script></head><body><p>x</p></body></html>";
    const [head, tail] = splitBodyScripts(shell);
    expect(head).toBe("<html><head><script>1</script></head><body><p>x</p>");
    expect(tail).toBe("</body></html>");
  });

  it("returns the whole shell when there is no body", () => {
    expect(splitBodyScripts("<p>fragment</p>")).toEqual(["<p>fragment</p>", ""]);
  });

  it("round-trips: head + tail is the original", () => {
    const shell = "<body>a<script>b</script>c</body>";
    const [h, t] = splitBodyScripts(shell);
    expect(h + t).toBe(shell);
  });
});

describe("replaceTitle", () => {
  it("swaps the content and drops title attributes", () => {
    expect(replaceTitle('<head><title data-x="1">Old</title></head>', "New")).toBe("<head><title>New</title></head>");
  });

  it("leaves a shell without a title alone", () => {
    expect(replaceTitle("<head></head>", "New")).toBe("<head></head>");
  });

  it("does not interpret replacement patterns in the new title", () => {
    expect(replaceTitle("<title>Old</title>", "Cost $& $1 $$")).toBe("<title>Cost $& $1 $$</title>");
  });
});

describe("regionReplaceScript", () => {
  const payloadOf = (script: string): string => {
    const m = /var html=(".*?");var kinds=/s.exec(script);
    expect(m).not.toBeNull();
    return JSON.parse(m![1]);
  };

  it("embeds the HTML so it cannot close the script element or break the literal", () => {
    const html = `</script><script>alert(1)</script> & "q" \u2028 \u2029 <!-- x -->`;
    const script = regionReplaceScript("r1", html);
    // Only the wrapper's own tags may appear: the payload has no raw < or >.
    expect(script.match(/<\/script>/gi)).toHaveLength(1);
    expect(script.match(/<script/gi)).toHaveLength(1);
    expect(script).not.toContain("\u2028");
    expect(script).not.toContain("\u2029");
    expect(payloadOf(script)).toBe(html);
  });

  it("quotes the id as a JSON string", () => {
    const script = regionReplaceScript("a\"b'c\\d", "x");
    expect(script).toContain('var id="a\\"b\'c\\\\d";');
  });

  it("executes against a minimal DOM and replaces only the marked block", () => {
    // Run the generated script with stub DOM objects to check the splice logic.
    const comments = [
      { nodeType: 8, nodeValue: "suspense:r1", nextSibling: null as any },
      { nodeType: 8, nodeValue: "/suspense:r1", nextSibling: null as any },
    ];
    comments[0].nextSibling = { nodeType: 3, nodeValue: "fallback", nextSibling: comments[1] };
    const calls: string[] = [];
    let idx = -1;
    const doc = {
      documentElement: {},
      createTreeWalker: () => ({
        get currentNode() {
          return comments[idx];
        },
        nextNode: () => ++idx < comments.length,
      }),
      createRange: () => ({
        setStartAfter: (n: any) => calls.push("start:" + n.nodeValue),
        setEndBefore: (n: any) => calls.push("end:" + n.nodeValue),
        deleteContents: () => calls.push("delete"),
        insertNode: (n: any) => calls.push("insert:" + n),
      }),
      createElement: () => {
        const t: any = { content: "<c>", set innerHTML(v: string) { t.content = v; } };
        return t;
      },
    };
    const code = regionReplaceScript("r1", "<b>done</b>").replace(/^<script[^>]*>/, "").replace(/<\/script>$/, "");
    new Function("document", "NodeFilter", code)(doc, { SHOW_COMMENT: 128 });
    expect(calls).toEqual(["start:suspense:r1", "end:/suspense:r1", "delete", "insert:<b>done</b>"]);
  });
});

describe("spliceShell", () => {
  it("replaces the baked content of a marker and keeps the markers and the rest of the shell", () => {
    const shell = `<main>${suspense("a", "loading")}<footer>f</footer></main>`;
    expect(splice(shell, { a: "<p>done</p>" })).toBe(`<main>${suspense("a", "<p>done</p>")}<footer>f</footer></main>`);
  });

  it("keeps the baked fallback of a region that produced no HTML", () => {
    const shell = `${suspense("a", "fb-a")}|${suspense("b", "fb-b")}`;
    expect(splice(shell, { b: "B" })).toBe(`${suspense("a", "fb-a")}|${suspense("b", "B")}`);
  });

  it("descends into a suspense fallback to fill nested regions", () => {
    const shell = suspense("outer", `shell ${region("inner", "baked-inner")} end`);
    expect(splice(shell, { inner: "NEW" })).toBe(suspense("outer", `shell ${region("inner", "NEW")} end`));
  });

  it("lets the outer region win over nested ones when both have HTML", () => {
    const shell = suspense("outer", region("inner", "baked"));
    expect(splice(shell, { outer: "OUT", inner: "IN" })).toBe(suspense("outer", "OUT"));
  });

  it("leaves a standalone runtime region empty when it produced no HTML (same as the Go host)", () => {
    const shell = `a${region("r", suspense("s", "baked"))}b`;
    expect(splice(shell, { s: "IGNORED" })).toBe(`a${region("r")}b`);
  });
});

const SECRET = "krate-test-flash-secret-0123456789";

// The same vector is pinned in internal/build/serve_flash_test.go: what the Go
// host signs verifies here, and what is signed here verifies there.
const VECTOR_PAYLOAD = "eyJhIjoiYWN0MSIsInIiOnsiZXJyb3IiOiJOYW1lIGlzIHJlcXVpcmVkIn19";
const VECTOR_COOKIE = VECTOR_PAYLOAD + ".zLQd73rhp1HN8J0vNvSeLqvlCqquygUX-mXuuN24dlY";

async function flashCookie(payload: unknown, secret: string | null = SECRET): Promise<string> {
  const b64 = Buffer.from(JSON.stringify(payload), "utf-8").toString("base64url");
  return secret === null ? b64 : signFlash(secret, b64);
}

describe("requestFlash", () => {
  const payload = { a: "act1", r: { error: "Name is required" } };

  it("reads the signed flash cookie among other cookies", async () => {
    const got = await requestFlash(`theme=dark; krate_flash=${await flashCookie(payload)}; x=1`, SECRET);
    expect(got?.payload).toEqual(payload);
  });

  it("accepts a quoted value and returns the payload part as raw", async () => {
    const got = await requestFlash(`krate_flash="${await flashCookie(payload)}"`, SECRET);
    expect(got?.payload).toEqual(payload);
    expect(got?.raw).toBe(VECTOR_PAYLOAD);
  });

  it("signs and verifies the same bytes as the Go host", async () => {
    expect(await signFlash(SECRET, VECTOR_PAYLOAD)).toBe(VECTOR_COOKIE);
    expect(await verifyFlash(SECRET, VECTOR_COOKIE)).toBe(VECTOR_PAYLOAD);
    expect(flashPayloadOf("act1", { error: "Name is required" })).toBe(VECTOR_PAYLOAD);
  });

  it("never trusts an unsigned, forged or foreign-signed cookie", async () => {
    const unsigned = await flashCookie(payload, null);
    expect(await requestFlash(`krate_flash=${unsigned}`, SECRET)).toBeNull();
    expect(await requestFlash(`krate_flash=${unsigned}.`, SECRET)).toBeNull();
    expect(await requestFlash(`krate_flash=${unsigned}.deadbeef`, SECRET)).toBeNull();
    expect(await requestFlash(`krate_flash=${await flashCookie(payload, "another-secret-another-secret")}`, SECRET)).toBeNull();
    // A valid signature does not carry over to a different payload.
    const sig = VECTOR_COOKIE.slice(VECTOR_COOKIE.indexOf(".") + 1);
    const other = Buffer.from(JSON.stringify({ a: "act1", r: { error: "Injected notice" } })).toString("base64url");
    expect(await requestFlash(`krate_flash=${other}.${sig}`, SECRET)).toBeNull();
    // Without a secret nothing is trusted, signed or not.
    expect(await requestFlash(`krate_flash=${VECTOR_COOKIE}`, null)).toBeNull();
    expect(await requestFlash(`krate_flash=${VECTOR_COOKIE}`, "")).toBeNull();
  });

  it("does not match a cookie that merely ends with the name", async () => {
    expect(await requestFlash(`other_krate_flash=${await flashCookie(payload)}`, SECRET)).toBeNull();
  });

  it("returns null for no header, an empty value, bad base64, bad JSON or no action id", async () => {
    expect(await requestFlash(null, SECRET)).toBeNull();
    expect(await requestFlash("krate_flash=", SECRET)).toBeNull();
    expect(await requestFlash("krate_flash=%%%", SECRET)).toBeNull();
    expect(await requestFlash(`krate_flash=${await signFlash(SECRET, Buffer.from("not json").toString("base64url"))}`, SECRET)).toBeNull();
    expect(await requestFlash(`krate_flash=${await flashCookie({ a: "", r: "x" })}`, SECRET)).toBeNull();
    expect(await requestFlash(`krate_flash=${await flashCookie({ r: "x" })}`, SECRET)).toBeNull();
  });

  it("decodes multi-byte text", async () => {
    const got = await requestFlash(`krate_flash=${await flashCookie({ a: "a", r: "café €" })}`, SECRET);
    expect(got?.payload.r).toBe("café €");
  });
});

describe("flashMessages", () => {
  it("collects strings in a stable order (sorted keys, array order), trimmed", () => {
    expect(flashMessages({ b: " second ", a: "first", c: ["x", "", "  ", "y"], n: 5, ok: true })).toEqual(["first", "second", "x", "y"]);
  });

  it("stops descending after a few levels", () => {
    expect(flashMessages({ a: { b: { c: { d: "too deep" } } } })).toEqual([]);
    expect(flashMessages({ a: { b: { c: "kept" } } })).toEqual(["kept"]);
  });

  it("handles a bare string and empty values", () => {
    expect(flashMessages("hi")).toEqual(["hi"]);
    expect(flashMessages(null)).toEqual([]);
    expect(flashMessages(undefined)).toEqual([]);
  });
});

describe("injectFlashNotice", () => {
  const page = `<body><form method="post" action="/__krate/action/save"><input name="n"></form><form action="/__krate/action/other"></form></body>`;

  it("inserts the notice right after the opening tag of the form that produced it", () => {
    const out = injectFlashNotice(page, "RAW", { a: "save", r: { error: "Bad <name>" } });
    expect(out).not.toBeNull();
    const first = out!.indexOf("krate-flash");
    expect(first).toBeGreaterThan(out!.indexOf('action="/__krate/action/save"'));
    expect(first).toBeLessThan(out!.indexOf('<input name="n">'));
    // the other form is untouched
    expect(out!.match(/krate-flash"/g)).toHaveLength(1);
  });

  it("escapes the message, the action id and the cookie value", () => {
    const out = injectFlashNotice(
      `<form action="/__krate/action/a&quot;b"></form>`,
      'v"><img src=x onerror=alert(1)>',
      { a: 'a&quot;b', r: "<script>alert(1)</script>" },
    )!;
    expect(out).not.toContain("<script>");
    expect(out).not.toContain("<img src=x");
    expect(out).toContain("&lt;script&gt;");
    expect(out).toContain('role="alert"');
  });

  it("matches an action id that contains regex characters literally", () => {
    const html = `<form action="/__krate/action/a.b+c"></form><form action="/__krate/action/aXb+c"></form>`;
    const out = injectFlashNotice(html, "v", { a: "a.b+c", r: "msg" })!;
    expect(out.indexOf("krate-flash")).toBeLessThan(out.indexOf("aXb+c"));
    expect(out.match(/krate-flash"/g)).toHaveLength(1);
  });

  it("returns null when there is nothing to show or no matching form", () => {
    expect(injectFlashNotice(page, "v", { a: "save", r: 7 })).toBeNull();
    expect(injectFlashNotice(page, "v", { a: "missing", r: "msg" })).toBeNull();
  });
});
