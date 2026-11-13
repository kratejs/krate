// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  injectFlashNotice,
  regionReplaceScript,
  requestFlash,
  signFlash,
} from "./handler-html.js";



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
});

describe("injectFlashNotice", () => {

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
});
