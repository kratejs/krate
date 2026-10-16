// Ambient fallback for Krate's generated `krate/content` module.
//
// `krate build` / `krate types` writes a precise, schema-derived declaration
// beside the source (`krate-content.d.ts`, gitignored). This loose declaration
// is committed so `tsc` still resolves `import { getCollection } from
// "krate/content"` when no build has run - e.g. `pnpm typecheck` in CI. When the
// generated file is present the two merge (function overloads), so builds get
// the precise per-collection types.
declare module "krate/content" {
  export interface KrateContentEntry {
    slug: string;
    path: string;
    body: string;
    html: string;
    data: Record<string, any>;
  }
  export function getCollection(name: string): KrateContentEntry[];
}

declare module "@krate/content" {
  export * from "krate/content";
}
