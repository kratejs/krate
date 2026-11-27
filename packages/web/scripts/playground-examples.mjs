// Loads src/playground/examples.ts (plain data, no TypeScript syntax) from Node.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
export const examplesFile = join(here, "../src/playground/examples.ts");

const demosDir = join(here, "../src/demos");

// One playground example per demo: the demo's own files under src/components,
// a page that renders it, and the shared kit styles, so "Open in playground"
// runs the same code the demo page shows.
function demoExample(d, baseCss) {
  const dir = join(demosDir, d.slug);
  const files = {
    "src/pages/index.tsx": `import Demo from "../components/demo";\nimport "../styles.css";\n\nexport default function Home() {\n  return (\n    <main>\n      <h1>${d.title}</h1>\n      <Demo />\n    </main>\n  );\n}\n`,
  };
  for (const name of d.files) files["src/components/" + name] = readFileSync(join(dir, name), "utf8");
  files["src/styles.css"] = baseCss + "main { max-width: 46rem; }\n" + readFileSync(join(demosDir, "kit.css"), "utf8");
  for (const id of d.detailPages || []) {
    files[`src/pages/demos/${d.slug}/${id}.tsx`] =
      `import Detail from "../../../components/detail";\nimport "../../../styles.css";\n\nexport default function Page() {\n  return (\n    <main>\n      <Detail id="${id}" />\n    </main>\n  );\n}\n`;
  }
  return {
    id: "demo-" + d.slug,
    title: "Demo: " + d.title,
    description: d.blurb,
    tags: ["demo", ...d.tags],
    js: d.js,
    entry: "/",
    open: "src/components/demo.tsx",
    files,
  };
}

export async function loadExamples() {
  const source = readFileSync(examplesFile, "utf8");
  const mod = await import("data:text/javascript;base64," + Buffer.from(source).toString("base64"));
  const { demos } = await loadDemos();
  const examples = [...mod.examples, ...demos.map((d) => demoExample(d, mod.baseCss))];
  return { examples, defaultExampleId: mod.defaultExampleId };
}

export const demosFile = join(here, "../src/demos/registry.ts");

// The demos gallery registry (src/demos/registry.ts, plain data).
export async function loadDemos() {
  const source = readFileSync(demosFile, "utf8");
  const mod = await import("data:text/javascript;base64," + Buffer.from(source).toString("base64"));
  return { demos: mod.demos };
}
