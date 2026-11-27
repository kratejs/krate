import { createCSSChoice } from "@krate/runtime";

// No signal, no handler, no script: the choice compiles to a hidden radio
// group and :has() selectors, so it works with JavaScript turned off.
export function ThemeSwitch() {
  const theme = createCSSChoice("light", ["light", "dark"]);
  return (
    <div class="ts">
      <div class="ts-seg">
        <button type="button" {...theme.trigger("light")}>Light</button>
        <button type="button" {...theme.trigger("dark")}>Dark</button>
      </div>
      <div {...theme.panel("light")} class="ts-card ts-light">Pure CSS, light.</div>
      <div {...theme.panel("dark")} class="ts-card ts-dark">Pure CSS, dark.</div>
    </div>
  );
}
