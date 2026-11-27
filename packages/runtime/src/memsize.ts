// Approximate retained size of cached values, for byte-bounded caches. It
// counts string characters as bytes (V8 stores ASCII text one byte per
// character), adds a small fixed cost per object, array, key and number, and
// stops at maxDepth so a deep or cyclic value costs a bounded walk.

const OBJECT_COST = 32;
const SLOT_COST = 8;

export function approxBytes(value: unknown, maxDepth = 8): number {
  const seen = new Set<object>();
  const walk = (v: unknown, depth: number): number => {
    switch (typeof v) {
      case "string":
        return v.length + 16;
      case "number":
      case "boolean":
      case "undefined":
      case "symbol":
        return SLOT_COST;
      case "bigint":
        return 16;
      case "function":
        return OBJECT_COST;
    }
    if (v === null) return SLOT_COST;
    const o = v as object;
    if (seen.has(o)) return SLOT_COST;
    seen.add(o);
    if (ArrayBuffer.isView(o)) return o.byteLength + OBJECT_COST;
    if (o instanceof ArrayBuffer) return o.byteLength + OBJECT_COST;
    if (depth >= maxDepth) return OBJECT_COST;
    let n = OBJECT_COST;
    if (Array.isArray(o)) {
      for (const item of o) n += SLOT_COST + walk(item, depth + 1);
      return n;
    }
    if (o instanceof Map) {
      for (const [k, item] of o) n += walk(k, depth + 1) + walk(item, depth + 1);
      return n;
    }
    if (o instanceof Set) {
      for (const item of o) n += SLOT_COST + walk(item, depth + 1);
      return n;
    }
    for (const k of Object.keys(o)) n += k.length + SLOT_COST + walk((o as Record<string, unknown>)[k], depth + 1);
    return n;
  };
  return walk(value, 0);
}

/** The default memory budget of the render caches, in bytes. */
export const DEFAULT_CACHE_BYTES = 64 * 1024 * 1024;

/**
 * The byte budgets of the in-memory caches, from KRATE_SSR_CACHE_BYTES (the
 * total for one process; 0 or unset is the default). Rendered pages get half,
 * PPR regions and the data cache a quarter each.
 */
export function cacheByteBudgets(total?: number): { pages: number; regions: number; data: number } {
  const t = total && total > 0 ? total : DEFAULT_CACHE_BYTES;
  return { pages: Math.floor(t / 2), regions: Math.floor(t / 4), data: Math.floor(t / 4) };
}

export function cacheBytesFromEnv(env: Record<string, string | undefined> | undefined): number {
  const n = Number(env?.KRATE_SSR_CACHE_BYTES);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
}
