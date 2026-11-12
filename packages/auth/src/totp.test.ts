import { describe, expect, it } from "vitest";
import { base32Decode, base32Encode, consumeBackupCode, generateBackupCodes, generateSecret, hotp, normalizeBackupCode, otpauthUrl, totp, verifyTotp } from "./totp";

const sha1Key = base32Encode(new TextEncoder().encode("12345678901234567890"));
const sha256Key = base32Encode(new TextEncoder().encode("12345678901234567890123456789012"));
const sha512Key = base32Encode(new TextEncoder().encode("1234567890123456789012345678901234567890123456789012345678901234"));

describe("base32", () => {
  it("matches RFC 4648 vectors", () => {
    const enc = (s: string) => base32Encode(new TextEncoder().encode(s));
    expect(enc("")).toBe("");
    expect(enc("f")).toBe("MY");
    expect(enc("fo")).toBe("MZXQ");
    expect(enc("foo")).toBe("MZXW6");
    expect(enc("foob")).toBe("MZXW6YQ");
    expect(enc("fooba")).toBe("MZXW6YTB");
    expect(enc("foobar")).toBe("MZXW6YTBOI");
  });

  it("round-trips and tolerates spacing and case", () => {
    for (let n = 1; n < 40; n++) {
      const bytes = new Uint8Array(n).map((_, i) => (i * 53 + n) & 0xff);
      expect(Array.from(base32Decode(base32Encode(bytes)))).toEqual(Array.from(bytes));
    }
    expect(new TextDecoder().decode(base32Decode("mzxw 6ytb-oi"))).toBe("foobar");
    expect(() => base32Decode("!!!")).toThrow();
  });
});

describe("totp (RFC 6238 appendix B)", () => {
  const cases: Array<[number, string, string, string]> = [
    [59, "94287082", "46119246", "90693936"],
    [1111111109, "07081804", "68084774", "25091201"],
    [1111111111, "14050471", "67062674", "99943326"],
    [1234567890, "89005924", "91819424", "93441116"],
    [2000000000, "69279037", "90698825", "38618901"],
    [20000000000, "65353130", "77737706", "47863826"],
  ];
  for (const [time, s1, s256, s512] of cases) {
    it(`t=${time}`, () => {
      const now = time * 1000;
      expect(totp(sha1Key, { digits: 8, period: 30, algorithm: "SHA1" }, now)).toBe(s1);
      expect(totp(sha256Key, { digits: 8, period: 30, algorithm: "SHA256" }, now)).toBe(s256);
      expect(totp(sha512Key, { digits: 8, period: 30, algorithm: "SHA512" }, now)).toBe(s512);
    });
  }

  it("matches the RFC 4226 hotp vectors", () => {
    const key = new TextEncoder().encode("12345678901234567890");
    const expected = ["755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"];
    expected.forEach((code, counter) => expect(hotp(key, counter, { digits: 6, algorithm: "SHA1" })).toBe(code));
  });
});

describe("verifyTotp", () => {
  const opts = { digits: 6, period: 30, algorithm: "SHA1" as const, window: 1 };
  const now = 1_700_000_000_000;

  it("accepts the current and neighbouring steps only", () => {
    const secret = generateSecret();
    const step = (offset: number) => totp(secret, opts, now + offset * 30_000);
    expect(verifyTotp(step(0), secret, opts, now)).toBe(Math.floor(now / 30_000));
    expect(verifyTotp(step(-1), secret, opts, now)).not.toBeNull();
    expect(verifyTotp(step(1), secret, opts, now)).not.toBeNull();
    expect(verifyTotp(step(-2), secret, opts, now)).toBeNull();
    expect(verifyTotp(step(2), secret, opts, now)).toBeNull();
  });

  it("rejects malformed input and wrong digits", () => {
    const secret = generateSecret();
    for (const bad of ["", "abc123", "12345", "1234567", "12 34"]) expect(verifyTotp(bad, secret, opts, now)).toBeNull();
    expect(verifyTotp(totp(secret, opts, now).split("").reverse().join("") + "0", secret, opts, now)).toBeNull();
  });

  it("allows spaces inside a code", () => {
    const secret = generateSecret();
    const code = totp(secret, opts, now);
    expect(verifyTotp(`${code.slice(0, 3)} ${code.slice(3)}`, secret, opts, now)).not.toBeNull();
  });

  it("refuses a step that was already used", () => {
    const secret = generateSecret();
    const code = totp(secret, opts, now);
    const counter = verifyTotp(code, secret, opts, now)!;
    expect(verifyTotp(code, secret, { ...opts, lastUsedCounter: counter }, now)).toBeNull();
    expect(verifyTotp(totp(secret, opts, now + 30_000), secret, { ...opts, lastUsedCounter: counter }, now + 30_000)).not.toBeNull();
  });

  it("produces secrets sized for the algorithm", () => {
    expect(base32Decode(generateSecret("SHA1"))).toHaveLength(20);
    expect(base32Decode(generateSecret("SHA256"))).toHaveLength(32);
    expect(base32Decode(generateSecret("SHA512"))).toHaveLength(64);
    expect(generateSecret()).not.toBe(generateSecret());
  });
});

describe("otpauth url", () => {
  it("encodes the label and parameters", () => {
    const url = otpauthUrl({ secret: "ABC", account: "ada+test@example.com", issuer: "My App", digits: 6, period: 30, algorithm: "SHA1" });
    expect(url.startsWith("otpauth://totp/My%20App:ada%2Btest%40example.com?")).toBe(true);
    const params = new URL(url).searchParams;
    expect(params.get("secret")).toBe("ABC");
    expect(params.get("issuer")).toBe("My App");
    expect(params.get("algorithm")).toBe("SHA1");
    expect(params.get("digits")).toBe("6");
    expect(params.get("period")).toBe("30");
  });
});

describe("backup codes", () => {
  it("generates unique, hashed, single-use codes", () => {
    const { plain, stored } = generateBackupCodes(10);
    expect(plain).toHaveLength(10);
    expect(new Set(plain).size).toBe(10);
    for (const code of plain) expect(code).toMatch(/^[A-Z2-7]{5}-[A-Z2-7]{5}$/);
    for (const hash of stored) expect(hash).not.toContain(plain[0]!);
    const after = consumeBackupCode(plain[3]!, stored)!;
    expect(after).toHaveLength(9);
    expect(consumeBackupCode(plain[3]!, after)).toBeNull();
    expect(consumeBackupCode(plain[4]!.toLowerCase().replace("-", " "), after)).toHaveLength(8);
    expect(consumeBackupCode("AAAAA-AAAAA", stored)).toBeNull();
    expect(consumeBackupCode("short", stored)).toBeNull();
    expect(normalizeBackupCode("ab cd-ef")).toBe("ABCDEF");
  });

  it("generates nothing when asked for zero", () => {
    expect(generateBackupCodes(0)).toEqual({ plain: [], stored: [] });
  });
});
