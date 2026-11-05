// A bounded cache that one client cannot flush. A plain LRU drops the hot
// entries as soon as enough new keys arrive between two hits, and on an open
// dynamic route ("/blog/[slug]") any slug renders, so a client can mint new
// keys at will. This is a segmented LRU: a new entry starts in a small
// probation segment and moves to the protected segment on its first hit, so a
// flood of one-off keys only churns probation and never reaches the entries
// that were requested twice.

export class SegmentedLRU<V> {
  private probation = new Map<string, V>();
  private protectedSeg = new Map<string, V>();
  private readonly max: number;
  private readonly protectedMax: number;

  constructor(maxSize: number) {
    this.max = Math.max(1, Math.floor(maxSize));
    // At least one probation slot, so a new entry can always be admitted.
    this.protectedMax = Math.min(this.max - 1, Math.max(1, Math.floor(this.max * 0.8)));
  }

  get size(): number {
    return this.probation.size + this.protectedSeg.size;
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
    if (this.protectedSeg.size > this.protectedMax) {
      // The coldest protected entry goes back to probation's newest slot.
      const demoted = this.protectedSeg.keys().next().value as string;
      const v = this.protectedSeg.get(demoted) as V;
      this.protectedSeg.delete(demoted);
      this.probation.set(demoted, v);
    }
    return cold;
  }

  /** Stores an entry. An existing key keeps its segment. Returns the evicted entries. */
  set(key: string, value: V): Array<[string, V]> {
    if (this.protectedSeg.has(key)) {
      this.protectedSeg.delete(key);
      this.protectedSeg.set(key, value);
      return [];
    }
    if (this.probation.has(key)) {
      this.probation.delete(key);
      this.probation.set(key, value);
      return [];
    }
    const evicted: Array<[string, V]> = [];
    while (this.size >= this.max) {
      const seg = this.probation.size > 0 ? this.probation : this.protectedSeg;
      const oldest = seg.keys().next().value as string;
      evicted.push([oldest, seg.get(oldest) as V]);
      seg.delete(oldest);
    }
    this.probation.set(key, value);
    return evicted;
  }

  delete(key: string): boolean {
    return this.protectedSeg.delete(key) || this.probation.delete(key);
  }

  clear(): void {
    this.probation.clear();
    this.protectedSeg.clear();
  }

  keys(): string[] {
    return [...this.probation.keys(), ...this.protectedSeg.keys()];
  }

  entries(): Array<[string, V]> {
    return [...this.probation.entries(), ...this.protectedSeg.entries()];
  }
}
