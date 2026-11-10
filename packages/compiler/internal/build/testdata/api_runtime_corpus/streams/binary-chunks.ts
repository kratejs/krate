export async function GET() {
  const s = new ReadableStream({
    async start(c) {
      for (let i = 0; i < 4; i++) { const b = new Uint8Array(64); for (let j = 0; j < 64; j++) b[j] = (i * 64 + j) & 255; c.enqueue(b); await new Promise((r) => setTimeout(r, 1)); }
      c.close();
    },
  });
  return new Response(s, { headers: { "content-type": "application/octet-stream" } });
}
