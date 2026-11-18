// Island state for browser tests. A Preact island has no element of its own
// (its nodes sit between <!--krate-island:...--> and <!--/krate-island-->);
// the loader lists every island in window.__krateIslands with its status. A
// real-React island and an external island are elements marked
// data-krate-island-hydrated.

// waitHydrated waits until sel is inside `depth` hydrated islands, or, with
// no selector, until any island hydrated.
export async function waitHydrated(page, sel = null, { depth = 1, timeout = 15000 } = {}) {
  await page.waitForFunction(
    ([sel, depth]) => {
      const list = window.__krateIslands || [];
      if (!sel) return list.some((i) => i.status === "hydrated") || !!document.querySelector("[data-krate-island-hydrated]");
      const el = document.querySelector(sel);
      if (!el) return false;
      let n = list.filter((i) => i.status === "hydrated" && i.contains(el)).length;
      for (let p = el; p; p = p.parentElement) if (p.hasAttribute && p.hasAttribute("data-krate-island-hydrated") && !list.some((i) => i.el === p)) n++;
      return n >= depth;
    },
    [sel, depth],
    { timeout },
  );
}
