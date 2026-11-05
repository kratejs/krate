import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { krateHighlight, krateHighlightInit } from "./highlight-bind";

// A fake language chunk: wraps each line in a span so output is easy to read.
function chunk(calls: string[]) {
  return {
    trimSpace: (s: string) => s.trim(),
    highlight: (code: string, lang: string) => {
      calls.push(code);
      if (code.length > 100) return null;
      return code
        .split("\n")
        .map((l) => `<span class="line"><span class="cl"><span class="k">${l}</span></span></span>`)
        .join("");
    },
  };
}

function mount(initial: string, lang = "ts"): { code: HTMLElement; start: Comment; set: (s: string) => void } {
  document.body.innerHTML = `<pre class="chroma"><code data-krate-hl="${lang}" class="language-${lang}"><!--k:b-->${initial}<!--/k:b--></code></pre>`;
  const code = document.querySelector("code") as HTMLElement;
  const start = code.firstChild as Comment;
  // What kbindText does: update the text node after the start marker, or add one.
  const set = (s: string) => {
    if (start.nextSibling && start.nextSibling.nodeType === 3) start.nextSibling.textContent = s;
    else start.parentNode!.insertBefore(document.createTextNode(s), start.nextSibling);
  };
  return { code, start, set };
}

const settle = async (ms: number) => {
  await vi.advanceTimersByTimeAsync(ms);
  await Promise.resolve();
};

describe("krateHighlightInit", () => {
  const calls: string[] = [];
  beforeEach(() => {
    calls.length = 0;
    vi.useFakeTimers();
    window.__KRATE_HL = { ts: "/chunks/hl-ts.js" };
    window.__KRATE_HL_LOAD = async () => chunk(calls);
  });
  afterEach(() => {
    vi.useRealTimers();
    delete window.__KRATE_HL;
    delete window.__KRATE_HL_LOAD;
    document.body.innerHTML = "";
  });

  it("shows the plain text, then swaps in highlighted markup", async () => {
    const { code } = mount("let a = 1");
    krateHighlightInit();
    expect(code.querySelector(".line")).toBeNull();
    await settle(1);
    expect(code.innerHTML).toContain('<span class="k">let a = 1</span>');
    expect(calls).toEqual(["let a = 1"]);
    expect(code.textContent).toBe("let a = 1");
  });

  it("debounces fast updates and keeps the markers", async () => {
    const { code, set } = mount("a");
    krateHighlightInit();
    await settle(1);
    for (const v of ["b", "bc", "bcd"]) {
      set(v);
      await Promise.resolve();
      await settle(30);
    }
    expect(code.querySelector(".line")).toBeNull();
    expect(code.textContent).toBe("bcd");
    await settle(200);
    expect(calls).toEqual(["a", "bcd"]);
    expect(code.textContent).toBe("bcd");
    expect(code.firstChild!.nodeType).toBe(8);
    expect(code.lastChild!.nodeType).toBe(8);
  });

  it("drops the previous highlighted markup when the slot writes new text", async () => {
    const { code, set } = mount("one\ntwo");
    krateHighlightInit();
    await settle(1);
    expect(code.querySelectorAll(".line")).toHaveLength(2);
    set("three");
    await settle(0);
    expect(code.textContent).toBe("three");
    await settle(200);
    expect(code.querySelectorAll(".line")).toHaveLength(1);
    expect(code.textContent).toBe("three");
  });

  it("does not highlight again when the code has not changed", async () => {
    document.body.innerHTML =
      '<pre class="chroma"><code data-krate-hl="ts"><!--k:b--><span class="line"><span class="cl">x</span></span><!--/k:b--></code></pre>';
    const code = document.querySelector("code") as HTMLElement;
    krateHighlightInit();
    await settle(500);
    expect(calls).toEqual([]);
    // a second init is a no-op too
    krateHighlightInit();
    await settle(500);
    expect(calls).toEqual([]);
    expect(code.innerHTML).toContain("line");
  });

  it("leaves plain text when the text is too large or the language is unavailable", async () => {
    const big = mount("x".repeat(200));
    krateHighlightInit();
    await settle(1);
    expect(big.code.querySelector(".line")).toBeNull();
    expect(big.code.textContent).toBe("x".repeat(200));
    const none = mount("let a", "zz");
    krateHighlightInit();
    await settle(1);
    expect(none.code.querySelector(".line")).toBeNull();
  });

  it("binds code that is added to the page later", async () => {
    document.body.innerHTML = "<main></main>";
    krateHighlightInit();
    const main = document.querySelector("main") as HTMLElement;
    main.innerHTML = '<pre class="chroma"><code data-krate-hl="ts"><!--k:z-->late<!--/k:z--></code></pre>';
    await settle(5);
    expect(main.querySelector(".line")).not.toBeNull();
    expect(calls).toEqual(["late"]);
  });

  it("highlights on demand through krateHighlight", async () => {
    expect(await krateHighlight("  a\n", "ts")).toContain('<span class="k">a</span>');
    expect(await krateHighlight("a", "unknown")).toBeNull();
  });
});
