// Runs the shared golden wire vectors (packages/native/wire/testdata) through
// the codec bundled in hostchild.mjs. Usage: <runtime> vectors.mjs <vector dir> <hostchild.mjs>
import { readdirSync, readFileSync, existsSync } from "node:fs";
import { pathToFileURL } from "node:url";

const [dir, mod] = process.argv.slice(2);
const { decodeFrame, encodeFrame } = await import(pathToFileURL(mod).href);
const ESCAPED = ["$b$", "$d$", "$m$", "$s$"];
const own = (o, k, v) => Object.defineProperty(o, k, { value: v, enumerable: true, writable: true, configurable: true });

function fromDescription(v) {
  if (Array.isArray(v)) return v.map(fromDescription);
  if (v === null || typeof v !== "object") return v;
  const keys = Object.keys(v);
  if (keys.length === 1) {
    const k = keys[0], x = v[k];
    if (k === "$b") return new Uint8Array(Buffer.from(x, "base64"));
    if (k === "$d") return new Date(x);
    if (k === "$m") return new Map(x.map(([a, b]) => [fromDescription(a), fromDescription(b)]));
    if (k === "$s") return new Set(x.map(fromDescription));
    if (ESCAPED.includes(k)) {
      const inner = {};
      for (const ik of Object.keys(x)) own(inner, ik, fromDescription(x[ik]));
      return inner;
    }
  }
  const out = {};
  for (const k of keys) own(out, k, fromDescription(v[k]));
  return out;
}

// canon renders a value so that two equal values give equal text.
function canon(v) {
  if (v instanceof Uint8Array) return "b:" + Buffer.from(v).toString("base64");
  if (v instanceof Date) return "d:" + v.toISOString();
  if (v instanceof Map) return "m:[" + [...v].map(([a, b]) => canon(a) + "=>" + canon(b)).join(",") + "]";
  if (v instanceof Set) return "s:[" + [...v].map(canon).join(",") + "]";
  if (Array.isArray(v)) return "[" + v.map(canon).join(",") + "]";
  if (v && typeof v === "object") return "{" + Object.keys(v).map((k) => JSON.stringify(k) + ":" + canon(v[k])).join(",") + "}";
  if (Object.is(v, -0)) return "-0";
  return JSON.stringify(v);
}

let n = 0;
const bad = [];
for (const f of readdirSync(dir).filter((f) => f.endsWith(".bin")).sort()) {
  const name = f.slice(0, -4);
  const bin = new Uint8Array(readFileSync(`${dir}/${f}`));
  const want = fromDescription(JSON.parse(readFileSync(`${dir}/${name}.json`, "utf8")));
  const flagsFile = `${dir}/${name}.flags.json`;
  const flags = existsSync(flagsFile) ? JSON.parse(readFileSync(flagsFile, "utf8")) : {};
  try {
    const got = decodeFrame(bin);
    if (got.size !== bin.length) throw new Error("trailing bytes");
    if (canon(got.value) !== canon(want)) throw new Error("decoded value differs");
    if (flags.tsEncode !== false) {
      const again = encodeFrame(got.value);
      if (Buffer.compare(Buffer.from(again), Buffer.from(bin)) !== 0) throw new Error("re-encoded bytes differ");
    }
    n++;
  } catch (e) {
    bad.push(`${name}: ${e.message}`);
  }
}
if (bad.length) {
  console.log("FAIL\n" + bad.join("\n"));
  process.exit(1);
}
console.log("ok " + n);
