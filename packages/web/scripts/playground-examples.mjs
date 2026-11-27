// Loads src/playground/examples.ts (plain data, no TypeScript syntax) from Node.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
export const examplesFile = join(here, "../src/playground/examples.ts");

export async function loadExamples() {
  const source = readFileSync(examplesFile, "utf8");
  const mod = await import("data:text/javascript;base64," + Buffer.from(source).toString("base64"));
  return { examples: mod.examples, defaultExampleId: mod.defaultExampleId };
}
