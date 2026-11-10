export async function GET() {
  const n = (1 << 20) + 3;
  const b = new Uint8Array(n);
  let x = 1;
  for (let i = 0; i < n; i++) { x = (x * 1103515245 + 12345) & 0x7fffffff; b[i] = x >>> 16; }
  return new Response(b, { headers: { "content-type": "application/octet-stream" } });
}
