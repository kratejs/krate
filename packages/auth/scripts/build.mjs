import { rmSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const root = fileURLToPath(new URL("..", import.meta.url));
const shim = fileURLToPath(new URL("../src/shims/webcrypto.ts", import.meta.url));

rmSync(`${root}dist`, { recursive: true, force: true });

await build({
  absWorkingDir: root,
  entryPoints: {
    index: "src/index.ts",
    client: "src/client.ts",
    webauthn: "src/webauthn.ts",
    "adapters/memory": "src/adapters/memory.ts",
    "adapters/drizzle/index": "src/adapters/drizzle/index.ts",
    "adapters/drizzle/sqlite": "src/adapters/drizzle/sqlite.ts",
    "adapters/drizzle/pg": "src/adapters/drizzle/pg.ts",
    "adapters/drizzle/mysql": "src/adapters/drizzle/mysql.ts",
    "adapters/drizzle/ddl": "src/adapters/drizzle/ddl.ts",
  },
  outdir: "dist",
  bundle: true,
  splitting: true,
  format: "esm",
  platform: "neutral",
  target: "es2022",
  chunkNames: "chunks/[name]-[hash]",
  external: ["drizzle-orm", "drizzle-orm/*"],
  alias: {
    "@noble/hashes/crypto": shim,
    "@noble/ciphers/crypto": shim,
  },
  logLevel: "warning",
});
