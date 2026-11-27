// Writes the static JSON the "data" demo fetches (public/demos/data). The files
// are committed; run this after changing the catalogue below.
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const out = join(dirname(fileURLToPath(import.meta.url)), "../public/demos/data");
mkdirSync(join(out, "components"), { recursive: true });

const items = [
  ["tabs", "Tabs", "Layout", "Roving tabindex, arrow keys, zero JS pointer behaviour.", "CSS signals"],
  ["accordion", "Accordion", "Disclosure", "Single or multiple open sections with aria-expanded.", "CSS signals"],
  ["dialog", "Dialog", "Overlay", "Modal with focus trap, Escape to close and an inert page behind it.", "CSS signals"],
  ["sheet", "Sheet", "Overlay", "An edge-anchored dialog for drawers and side panels.", "CSS signals"],
  ["command", "Command", "Input", "Filterable command palette with combobox semantics.", "Signals"],
  ["select", "Select", "Input", "Searchable select with keyboard navigation.", "Signals"],
  ["slider", "Slider", "Input", "Range input styled to the theme, with an optional live handler.", "Native input"],
  ["switch", "Switch", "Input", "A native checkbox with role=switch.", "Native input"],
  ["otp-field", "OTP field", "Input", "One box per digit with paste and autofill support.", "Signals"],
  ["toast", "Toast", "Feedback", "Call toast() from any handler; announced politely.", "Signals"],
  ["tooltip", "Tooltip", "Overlay", "Shown on hover and focus, dismissed with Escape.", "Signals"],
  ["progress", "Progress", "Feedback", "Determinate progress built on native semantics.", "Pure CSS"],
];

writeFileSync(
  join(out, "components.json"),
  JSON.stringify(items.map(([id, name, group]) => ({ id, name, group }))),
);
for (const [id, name, group, summary, built] of items) {
  writeFileSync(
    join(out, "components", id + ".json"),
    JSON.stringify({ id, name, group, summary, builtWith: built, import: `import { ${name.replace(/\W/g, "")} } from "@krate/components";` }),
  );
}
console.log(`wrote ${items.length} components`);
