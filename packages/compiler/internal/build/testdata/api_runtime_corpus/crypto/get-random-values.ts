export async function GET() {
  const u8 = new Uint8Array(16), same = crypto.getRandomValues(u8) === u8;
  const kinds = [new Int8Array(4), new Uint16Array(4), new Int16Array(4), new Uint32Array(4), new Int32Array(4), new Uint8ClampedArray(4), new BigInt64Array(2), new BigUint64Array(2)];
  const lens = kinds.map((k) => { const r = crypto.getRandomValues(k as any); return [r.constructor.name, r.length]; });
  const max = new Uint8Array(65536);
  crypto.getRandomValues(max);
  const nonZero = max.some((x) => x !== 0);
  return Response.json({ same, lens, nonZero, zeroLen: crypto.getRandomValues(new Uint8Array(0)).length });
}
