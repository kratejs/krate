import { beforeAll, describe, expect, it } from "vitest";
import { SyntaxHighlight, renderToString } from "./server";
import { registerLanguage, type HighlightTable } from "./highlight";

const table: HighlightTable = {
  n: "Toy",
  t: ["nx", "mi", ""],
  s: [[["[a-z]+", 0], ["[0-9]+", 1], ["[ \\n]+", 2]]],
};

beforeAll(() => registerLanguage(table, ["toy"]));

const html = (n: any) => renderToString(n);

describe("SyntaxHighlight on the server", () => {
  it("highlights a language whose tables are loaded", () => {
    const out = html(SyntaxHighlight({ lang: "toy", children: "ab 12" }));
    expect(out).toContain('<pre class="chroma"><code class="language-toy">');
    expect(out).toContain('<span class="nx">ab</span>');
    expect(out).toContain('<span class="mi">12</span>');
  });

  it("marks lines and keeps the attribute the compiler writes", () => {
    const out = html(SyntaxHighlight({ lang: "toy", children: "a\nb", highlight: "2" }));
    expect(out).toContain('data-highlight-lines="2"');
    expect(out).toContain('<span class="line hl">');
  });

  it("writes plain escaped text for other languages and for unsafe input", () => {
    expect(html(SyntaxHighlight({ lang: "cobol", children: "<b>" }))).toContain("&lt;b&gt;");
    expect(html(SyntaxHighlight({ children: "a & b" }))).toContain("a &amp; b");
    const big = html(SyntaxHighlight({ lang: "toy", children: "a".repeat(70000) }));
    expect(big).not.toContain('class="line"');
    expect(big.length).toBeGreaterThan(70000);
    const long = html(SyntaxHighlight({ lang: "toy", children: "a".repeat(3000) }));
    expect(long).not.toContain('class="line"');
  });
});
