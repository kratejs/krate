// A bounded cache that one client cannot flush. A plain LRU drops the hot
// entries as soon as enough new keys arrive between two hits, and on an open
// dynamic route ("/blog/[slug]") any slug renders, so a client can mint new
// keys at will. This is a segmented LRU: a new entry starts in a small
// probation segment and moves to the protected segment on its first hit, so a
// flood of one-off keys only churns probation and never reaches the entries
// that were requested twice.

// With a byte limit, entries are also evicted (coldest first) until the
// entries' sizes fit, and an entry larger than the whole limit is not stored.
export interface ByteLimit<V> {
  maxBytes: number;
  sizeOf: (value: V) => number;
}

export class SegmentedLRU<V> {
  private probation = new Map<string, V>();
  private protectedSeg = new Map<string, V>();
  private readonly max: number;
  private readonly protectedMax: number;
  private readonly limit?: ByteLimit<V>;
  private readonly sizes = new Map<string, number>();
  private used = 0;

  constructor(maxSize: number, limit?: ByteLimit<V>) {
    if (limit && limit.maxBytes > 0) this.limit = limit;
    this.max = Math.max(1, Math.floor(maxSize));
    // At least one probation slot, so a new entry can always be admitted.
    this.protectedMax = Math.min(this.max - 1, Math.max(1, Math.floor(this.max * 0.8)));
  }

  get size(): number {
    return this.probation.size + this.protectedSeg.size;
  }

  /** The summed sizes of the entries (0 without a byte limit). */
  get bytes(): number {
    return this.used;
  }

  has(key: string): boolean {
    return this.protectedSeg.has(key) || this.probation.has(key);
  }

  /** Reads an entry without counting as a use. */
  peek(key: string): V | undefined {
    return this.protectedSeg.get(key) ?? this.probation.get(key);
  }

  /** Reads an entry and counts the use: probation entries are promoted. */
  get(key: string): V | undefined {
    const hot = this.protectedSeg.get(key);
    if (hot !== undefined) {
      this.protectedSeg.delete(key);
      this.protectedSeg.set(key, hot);
      return hot;
    }
    const cold = this.probation.get(key);
    if (cold === undefined) return undefined;
    this.probation.delete(key);
    if (this.protectedMax <= 0) {
      this.probation.set(key, cold);
      return cold;
    }
    this.protectedSeg.set(key, cold);
    if (this.protectedSeg.size > this.protectedMax) this.demoteColdest();
    if (this.limit) {
      // Protected entries keep at most 80% of the bytes too, so probation
      // always has room and new entries never flush the hot set.
      const cap = this.limit.maxBytes * 0.8;
      let hot = 0;
      for (const k of this.protectedSeg.keys()) hot += this.sizes.get(k) || 0;
      while (hot > cap && this.protectedSeg.size > 1) hot -= this.sizes.get(this.demoteColdest()) || 0;
    }
    return cold;
  }

  // demoteColdest moves the coldest protected entry to probation's newest
  // slot and returns its key.
  private demoteColdest(): string {
    const demoted = this.protectedSeg.keys().next().value as string;
    const v = this.protectedSeg.get(demoted) as V;
    this.protectedSeg.delete(demoted);
    this.probation.set(demoted, v);
    return demoted;
  }

  /** Stores an entry. An existing key keeps its segment. Returns the evicted entries. */
  set(key: string, value: V): Array<[string, V]> {
    const n = this.limit ? Math.max(0, this.limit.sizeOf(value)) : 0;
    if (this.limit && n > this.limit.maxBytes) {
      // Too large to cache: drop any older copy instead of keeping it stale.
      const old = this.peek(key);
      if (old === undefined) return [];
      this.delete(key);
      return [[key, old]];
    }
    const seg = this.protectedSeg.has(key) ? this.protectedSeg : this.probation.has(key) ? this.probation : null;
    if (seg) {
      seg.delete(key);
      seg.set(key, value);
      this.account(key, n);
      return this.fitBytes(key);
    }
    const evicted: Array<[string, V]> = [];
    while (this.size > 0 && (this.size >= this.max || (this.limit && this.used + n > this.limit.maxBytes))) {
      evicted.push(this.evictOldest());
    }
    this.probation.set(key, value);
    this.account(key, n);
    return evicted;
  }

  // evictOldest removes the coldest entry: probation first, then protected.
  private evictOldest(): [string, V] {
    const seg = this.probation.size > 0 ? this.probation : this.protectedSeg;
    const oldest = seg.keys().next().value as string;
    const v = seg.get(oldest) as V;
    seg.delete(oldest);
    this.account(oldest, 0);
    return [oldest, v];
  }

  // fitBytes evicts other entries, coldest first, until the sizes fit the
  // limit again.
  private fitBytes(keep: string): Array<[string, V]> {
    const evicted: Array<[string, V]> = [];
    if (!this.limit || this.used <= this.limit.maxBytes) return evicted;
    for (const seg of [this.probation, this.protectedSeg]) {
      for (const k of [...seg.keys()]) {
        if (this.used <= this.limit.maxBytes) return evicted;
        if (k === keep) continue;
        evicted.push([k, seg.get(k) as V]);
        seg.delete(k);
        this.account(k, 0);
      }
    }
    return evicted;
  }

  private account(key: string, n: number) {
    if (!this.limit) return;
    this.used -= this.sizes.get(key) || 0;
    if (n > 0) this.sizes.set(key, n);
    else this.sizes.delete(key);
    this.used += n;
  }

  delete(key: string): boolean {
    const gone = this.protectedSeg.delete(key) || this.probation.delete(key);
    if (gone) this.account(key, 0);
    return gone;
  }

  clear(): void {
    this.probation.clear();
    this.protectedSeg.clear();
    this.sizes.clear();
    this.used = 0;
  }

  keys(): string[] {
    return [...this.probation.keys(), ...this.protectedSeg.keys()];
  }

  entries(): Array<[string, V]> {
    return [...this.probation.entries(), ...this.protectedSeg.entries()];
  }
}
