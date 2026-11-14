interface Bucket {
  count: number;
  reset: number;
}

const buckets = new Map<string, Bucket>();
let lastSweep = 0;

export function clientIp(request: Request): string {
  const forwarded = request.headers.get("x-forwarded-for");
  if (forwarded) {
    const parts = forwarded.split(",").map((p) => p.trim()).filter(Boolean);
    if (parts.length > 0) return parts[parts.length - 1]!;
  }
  return request.headers.get("x-real-ip") ?? "unknown";
}

export function consume(key: string, max: number, windowSeconds: number, now = Date.now()): { allowed: boolean; retryAfter: number } {
  if (now - lastSweep > 60_000) {
    lastSweep = now;
    for (const [k, b] of buckets) if (b.reset <= now) buckets.delete(k);
  }
  const existing = buckets.get(key);
  if (!existing || existing.reset <= now) {
    buckets.set(key, { count: 1, reset: now + windowSeconds * 1000 });
    return { allowed: true, retryAfter: 0 };
  }
  existing.count++;
  if (existing.count > max) return { allowed: false, retryAfter: Math.max(1, Math.ceil((existing.reset - now) / 1000)) };
  return { allowed: true, retryAfter: 0 };
}

export function resetRateLimits(): void {
  buckets.clear();
}
