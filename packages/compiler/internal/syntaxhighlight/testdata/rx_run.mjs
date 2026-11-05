// Runs transpiled regexes over inputs and prints each match, for the
// differential regex test. usage: node rx_run.mjs <in.json> <out.json>
import fs from "node:fs";
const cases = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const out = cases.map((c) => {
  const re = new RegExp(c.src, "yu");
  return c.inputs.map((text) => {
    re.lastIndex = c.start || 0;
    const m = re.exec(text);
    return m === null ? null : [...m].map((g) => (g === undefined ? null : g));
  });
});
fs.writeFileSync(process.argv[3], JSON.stringify(out));
