import { useEffect, useMemo, useState } from "react";
import { log } from "../lib/log";

// An object literal created during render is a new dependency every render;
// a memoized object keeps its identity until its inputs change.
export default function EffectDepsObjectIdentity() {
  const [page, setPage] = useState(1);
  const [theme, setTheme] = useState("light");

  const inline = { page };
  const memo = useMemo(() => ({ page }), [page]);

  useEffect(() => {
    log("inline options effect", inline.page);
  }, [inline]);

  useEffect(() => {
    log("memo options effect", memo.page);
  }, [memo]);

  return (
    <div className={theme}>
      <p id="out">
        page {page} / {theme}
      </p>
      <button id="next" onClick={() => setPage(page + 1)}>
        next page
      </button>
      <button id="theme" onClick={() => setTheme(theme === "light" ? "dark" : "light")}>
        theme
      </button>
    </div>
  );
}
