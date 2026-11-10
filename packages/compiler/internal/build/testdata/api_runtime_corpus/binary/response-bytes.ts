export async function GET() { return new Response(new Uint8Array([0, 255, 254, 128, 65]), { headers: { "content-type": "application/octet-stream" } }); }
