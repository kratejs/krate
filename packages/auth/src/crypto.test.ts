import { describe, expect, it } from "vitest";
import { b64urlDecode, b64urlEncode, fromHex, hex, timingSafeEqual } from "./bytes";
import { ResponseCookies, defaultCookies, parseCookies, readChunked, serializeCookie } from "./cookies";
import { createCsrfToken, decodeToken, encodeToken, sealString, unsealString, verifyCsrfCookie } from "./crypto";

describe("bytes", () => {
  it("round-trips base64url for every length", () => {
    for (let n = 0; n < 40; n++) {
      const data = new Uint8Array(n).map((_, i) => (i * 37 + n) & 0xff);
      const enc = b64urlEncode(data);
      expect(enc).not.toMatch(/[+/=]/);
      expect(Array.from(b64urlDecode(enc))).toEqual(Array.from(data));
    }
  });

  it("matches the Buffer implementation", () => {
    const data = new Uint8Array([251, 255, 254, 0, 1, 2, 3, 250]);
    expect(b64urlEncode(data)).toBe(Buffer.from(data).toString("base64url"));
    expect(b64urlEncode("hello world")).toBe("aGVsbG8gd29ybGQ");
  });

  it("rejects invalid base64url", () => {
    expect(() => b64urlDecode("ab$d")).toThrow();
  });

  it("round-trips hex", () => {
    expect(hex(fromHex("00ff10ab"))).toBe("00ff10ab");
    expect(() => fromHex("abc")).toThrow();
    expect(() => fromHex("zz")).toThrow();
  });

  it("compares in constant time semantics", () => {
    expect(timingSafeEqual("abc", "abc")).toBe(true);
    expect(timingSafeEqual("abc", "abd")).toBe(false);
    expect(timingSafeEqual("abc", "abcd")).toBe(false);
  });
});

describe("tokens", () => {
  const secret = "a-secret-of-reasonable-length";

  it("encodes and decodes claims", async () => {
    const token = await encodeToken({ token: { sub: "u1", name: "Ada" }, secret, salt: "session", maxAge: 60 });
    expect(token.split(".")).toHaveLength(5);
    const claims = await decodeToken({ token, secret, salt: "session" });
    expect(claims).toMatchObject({ sub: "u1", name: "Ada" });
    expect(typeof claims?.exp).toBe("number");
    expect(claims?.jti).toBeTruthy();
  });

  it("does not leak claims in the token", async () => {
    const token = await encodeToken({ token: { email: "ada@example.com" }, secret, salt: "session" });
    expect(token).not.toContain("ada");
    expect(Buffer.from(token.split(".")[3]!, "base64url").toString()).not.toContain("ada@example.com");
  });

  it("rejects a different salt, secret or tampered token", async () => {
    const token = await encodeToken({ token: { sub: "u1" }, secret, salt: "session" });
    expect(await decodeToken({ token, secret, salt: "other" })).toBeNull();
    expect(await decodeToken({ token, secret: "another-secret", salt: "session" })).toBeNull();
    const parts = token.split(".");
    parts[3] = b64urlEncode(new Uint8Array(b64urlDecode(parts[3]!)).map((b) => b ^ 1));
    expect(await decodeToken({ token: parts.join("."), secret, salt: "session" })).toBeNull();
    parts[0] = b64urlEncode('{"alg":"none"}');
    expect(await decodeToken({ token: parts.join("."), secret, salt: "session" })).toBeNull();
  });

  it("rejects expired tokens", async () => {
    const now = Date.now();
    const token = await encodeToken({ token: { sub: "u1" }, secret, salt: "s", maxAge: 10, now });
    expect(await decodeToken({ token, secret, salt: "s", now: now + 5000 })).not.toBeNull();
    expect(await decodeToken({ token, secret, salt: "s", now: now + 11000 })).toBeNull();
  });

  it("supports secret rotation", async () => {
    const old = await encodeToken({ token: { sub: "u1" }, secret: "old-secret", salt: "s" });
    expect(await decodeToken({ token: old, secret: ["new-secret", "old-secret"], salt: "s" })).toMatchObject({ sub: "u1" });
    expect(await decodeToken({ token: old, secret: ["new-secret"], salt: "s" })).toBeNull();
  });

  it("refuses to encode without a secret and ignores garbage input", async () => {
    await expect(encodeToken({ token: {}, secret: "", salt: "s" })).rejects.toThrow();
    for (const bad of [undefined, null, "", "a.b", "a.b.c.d.e", "....."]) {
      expect(await decodeToken({ token: bad as string, secret, salt: "s" })).toBeNull();
    }
  });

  it("seals and unseals strings", () => {
    const sealed = sealString("JBSWY3DPEHPK3PXP", secret, "totp");
    expect(sealed).not.toContain("JBSWY");
    expect(unsealString(sealed, secret, "totp")).toBe("JBSWY3DPEHPK3PXP");
    expect(unsealString(sealed, secret, "other")).toBeNull();
    expect(unsealString(sealed, "wrong", "totp")).toBeNull();
    expect(unsealString("short", secret, "totp")).toBeNull();
    expect(unsealString(sealed, ["wrong", secret], "totp")).toBe("JBSWY3DPEHPK3PXP");
  });
});

describe("csrf", () => {
  it("verifies a cookie it issued", () => {
    const { token, cookieValue } = createCsrfToken("s3cret");
    expect(verifyCsrfCookie(cookieValue, "s3cret")).toEqual({ token, valid: true });
  });

  it("flags forged or foreign cookies", () => {
    const { cookieValue } = createCsrfToken("s3cret");
    expect(verifyCsrfCookie(cookieValue, "other")?.valid).toBe(false);
    expect(verifyCsrfCookie("abc|def", "s3cret")?.valid).toBe(false);
    expect(verifyCsrfCookie("nopipe", "s3cret")).toBeNull();
    expect(verifyCsrfCookie(undefined, "s3cret")).toBeNull();
  });
});

describe("cookies", () => {
  it("parses a Cookie header", () => {
    expect(parseCookies('a=1; b=hello%20world; c="q"; bad; =x')).toEqual({ a: "1", b: "hello world", c: "q" });
    expect(parseCookies(null)).toEqual({});
  });

  it("serializes attributes", () => {
    const c = serializeCookie("n", "v w", { httpOnly: true, secure: true, sameSite: "lax", maxAge: 60, domain: "example.com" });
    expect(c).toBe("n=v%20w; Path=/; Max-Age=60; Domain=example.com; HttpOnly; Secure; SameSite=Lax");
  });

  it("uses prefixed names only on secure origins", () => {
    const plain = defaultCookies({ secure: false });
    expect(plain.sessionToken.name).toBe("krate-auth.session-token");
    const secure = defaultCookies({ secure: true });
    expect(secure.sessionToken.name).toBe("__Secure-krate-auth.session-token");
    expect(secure.csrfToken.name).toBe("__Host-krate-auth.csrf-token");
    const withDomain = defaultCookies({ secure: true, domain: "example.com", prefix: "app" });
    expect(withDomain.csrfToken.name).toBe("__Secure-app.csrf-token");
    expect(withDomain.sessionToken.options.domain).toBe("example.com");
    expect(secure.pendingTwoFactor.options.maxAge).toBe(900);
  });

  it("chunks long values and cleans stale chunks", () => {
    const value = "x".repeat(9000);
    const jar = new ResponseCookies();
    jar.setChunked("sess", value, { path: "/" }, { "sess.5": "old", other: "keep" });
    const headers = new Headers();
    jar.apply(headers);
    const set = headers.getSetCookie();
    expect(set.filter((s) => s.startsWith("sess.")).length).toBeGreaterThanOrEqual(3);
    expect(set.some((s) => s.startsWith("sess.5=;"))).toBe(true);
    expect(set.some((s) => s.startsWith("other="))).toBe(false);
    const jarOut: Record<string, string> = {};
    for (const s of set) {
      const [pair] = s.split(";");
      const [k, v] = pair!.split("=");
      if (v) jarOut[k!] = v;
    }
    expect(readChunked(jarOut, "sess")).toBe(value);
  });

  it("keeps small values in one cookie and deletes old chunks", () => {
    const jar = new ResponseCookies();
    jar.setChunked("sess", "short", {}, { "sess.0": "a", "sess.1": "b" });
    const entries = jar.list();
    expect(entries[0]).toContain("sess=short");
    expect(entries.filter((e) => e.startsWith("sess.0=;") || e.startsWith("sess.1=;"))).toHaveLength(2);
    expect(readChunked({ sess: "one", "sess.0": "x" }, "sess")).toBe("one");
    expect(readChunked({}, "sess")).toBeUndefined();
  });
});
