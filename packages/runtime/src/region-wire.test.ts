import { describe, expect, it } from "vitest";
import { decodeFrames, encodeFrame, REGION_FRAMES_TYPE, wantsRegionFrames } from "./region-wire.js";

describe("region frames", () => {
  it("round-trips every field, with HTML kept raw", () => {
    const frame = {
      type: "region",
      id: "page",
      kind: "page",
      status: 404,
      html: `<p class="a">a & b < c > "d"   \u{1F600} é</p>`,
      cacheStatus: "stale",
      notFound: true,
      redirect: "/login?next=/a&b=1",
      denied: 403,
      title: "T <b>",
      head: `<meta name="x" content="y">`,
      noStore: true,
      errorCaught: true,
      fatal: true,
      error: "boom",
      digest: "abc123",
      stack: "at x\nat y",
      count: 7,
    };
    const buf = encodeFrame(frame);
    expect(buf.includes(Buffer.from("<p class=\"a\">a & b < c >"))).toBe(true);
    expect(decodeFrames(buf)).toEqual([frame]);
  });

  it("produces and parses the same bytes as the Go codec", () => {
    const hex = "000000190101c801000001610000000000000000083c623e263c2f623e";
    const frame = { type: "region", id: "a", status: 200, html: "<b>&</b>", notFound: true };
    expect(encodeFrame(frame).toString("hex")).toBe(hex);
    expect(decodeFrames(Buffer.from(hex, "hex"))).toEqual([frame]);
  });

  it("omits empty and unset fields and ignores unknown keys", () => {
    const [f] = decodeFrames(encodeFrame({ type: "skip", id: "x", html: "", cached: true, status: 0 }));
    expect(f).toEqual({ type: "skip", id: "x" });
  });

  it("decodes a stream of frames of every type", () => {
    const frames = [
      { type: "region", id: "a", status: 200, html: "<i>1</i>" },
      { type: "skip", id: "b" },
      { type: "error", id: "c", status: 500, error: "x", fatal: true },
      { type: "end", count: 3 },
    ];
    expect(decodeFrames(Buffer.concat(frames.map(encodeFrame)))).toEqual(frames);
  });

  it("carries very large HTML and multi-byte text byte for byte", () => {
    const html = "<td>é中\u{1F600}</td>".repeat(400_000);
    const [f] = decodeFrames(encodeFrame({ type: "region", id: "big", status: 200, html }));
    expect(f.html).toHaveLength(html.length);
    expect(f.html === html).toBe(true);
  });

  it("rejects unknown types and truncated or corrupt data", () => {
    expect(() => encodeFrame({ type: "nope" })).toThrow();
    const buf = encodeFrame({ type: "region", id: "a", status: 200, html: "<b>x</b>" });
    for (const cut of [1, 3, 4, 5, buf.length - 1]) expect(() => decodeFrames(buf.subarray(0, cut))).toThrow();
    const bad = Buffer.from(buf);
    bad[4] = 9;
    expect(() => decodeFrames(bad)).toThrow();
    const trailing = Buffer.concat([buf, Buffer.from([0, 0])]);
    expect(() => decodeFrames(trailing)).toThrow();
  });

  it("negotiates on the Accept header", () => {
    expect(wantsRegionFrames(undefined)).toBe(false);
    expect(wantsRegionFrames("*/*")).toBe(false);
    expect(wantsRegionFrames(REGION_FRAMES_TYPE)).toBe(true);
    expect(wantsRegionFrames(`text/html, ${REGION_FRAMES_TYPE}`)).toBe(true);
    expect(wantsRegionFrames(["text/html", REGION_FRAMES_TYPE])).toBe(true);
  });
});
