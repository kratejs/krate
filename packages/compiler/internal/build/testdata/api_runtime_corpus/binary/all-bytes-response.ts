export async function GET() {
  const b = new Uint8Array(256);
  for (let i = 0; i < 256; i++) b[i] = i;
  return new Response(b.buffer, { headers: { "content-type": "application/octet-stream" } });
}
