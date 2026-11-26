/** Colour mode used when nothing is stored. */
export type DefaultTheme = "light" | "dark" | "system";

/**
 * Resolves the mode to start in: the stored choice when it is valid, else the
 * configured default (`system` follows `prefers-color-scheme`).
 */
export function initialTheme(saved: string, def?: DefaultTheme): "light" | "dark" {
  if (saved === "dark" || saved === "light") return saved;
  if (def === "dark" || def === "light") return def;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

/** Inline script body (no wrapper) applying the initial mode to <html>. */
export function themePrePaintBody(storageKey: string, def?: DefaultTheme): string {
  const fallback =
    def === "dark" || def === "light"
      ? "'" + def + "'"
      : "window.matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light'";
  return (
    "try{var k=" + JSON.stringify(storageKey) +
    ";var s=localStorage.getItem(k);if(s!=='dark'&&s!=='light'){s=" + fallback + ";}" +
    "document.documentElement.setAttribute('data-theme',s);}catch(e){}"
  );
}
