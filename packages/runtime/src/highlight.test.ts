import { beforeAll, describe, expect, it } from "vitest";
import {
  HIGHLIGHT_MAX_LENGTH,
  HIGHLIGHT_MAX_LINE,
  escapeHTML,
  hasLanguage,
  highlight,
  parseLineRanges,
  plainLines,
  registerLanguage,
  trimSpace,
  type HighlightTable,
} from "./highlight";

// A tiny lexer in the exported table format: words are names, digits numbers,
// and an unmatched character is an error token.
const table: HighlightTable = {
  n: "Toy",
  t: ["nx", "mi", "", "c"],
  s: [
    [
      ["[a-z]+", 0],
      ["[0-9]+", 1],
      ["#[^\\n]*", 3],
      ["[ \\n]+", 2],
    ],
  ],
  e: 1,
};

beforeAll(() => registerLanguage(table, ["toy", "t"]));

describe("highlight", () => {
  it("writes chroma's markup: one line wrapper per line, one span per token", () => {
    expect(highlight("ab 12\nc", "toy")).toBe(
      '<span class="line"><span class="cl"><span class="nx">ab</span> <span class="mi">12</span>\n</span></span>' +
        '<span class="line"><span class="cl"><span class="nx">c</span></span></span>',
    );
  });

  it("marks lines and ignores malformed ranges", () => {
    const html = highlight("a\nb\nc", "toy", "2,x,5-3")!;
    expect(html.match(/class="line hl"/g)).toHaveLength(1);
    expect(html).toContain('<span class="line hl"><span class="cl"><span class="nx">b</span>');
    expect(parseLineRanges("1,3-5, 7 ,0,abc,4-2")).toEqual([[1, 1], [3, 5], [7, 7]]);
  });

  it("escapes text the way the compiler does", () => {
    expect(escapeHTML(`<a href="x">'&'</a>`)).toBe("&lt;a href=&#34;x&#34;&gt;&#39;&amp;&#39;&lt;/a&gt;");
    expect(highlight("#<b>", "toy")).toContain('<span class="c">#&lt;b&gt;</span>');
  });

  it("reports an unknown character as an error token and resumes", () => {
    expect(highlight("a!b", "toy")).toContain('<span class="err">!</span>');
  });

  it("returns null when it cannot highlight exactly", () => {
    expect(highlight("a", "nope")).toBeNull();
    expect(highlight("a".repeat(HIGHLIGHT_MAX_LENGTH + 1), "toy")).toBeNull();
    expect(highlight("a".repeat(HIGHLIGHT_MAX_LINE + 1), "toy")).toBeNull();
    expect(highlight("a\n".repeat(HIGHLIGHT_MAX_LENGTH / 2), "toy", undefined, { maxLength: 10 })).toBeNull();
    expect(hasLanguage(" TOY ")).toBe(true);
    expect(hasLanguage("nope")).toBe(false);
  });

  it("gives up when it runs out of time", () => {
    const text = "a 1 b 2\n".repeat(3000);
    expect(highlight(text, "toy", undefined, { budgetMs: -1 })).toBeNull();
    expect(highlight(text, "toy", undefined, { budgetMs: 10000 })).not.toBeNull();
  });

  it("falls back to plain lines for languages chroma does not know", () => {
    expect(plainLines("a<b\r\nc")).toBe(
      '<span class="line"><span class="cl">a&lt;b\n</span></span><span class="line"><span class="cl">c</span></span>',
    );
  });

  it("trims whitespace like Go's strings.TrimSpace", () => {
    expect(trimSpace("\u00a0 \n x \u2028")).toBe("x");
  });
});
