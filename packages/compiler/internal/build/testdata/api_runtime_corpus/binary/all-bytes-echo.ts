export async function POST(request) {
  return new Response(await request.arrayBuffer(), { headers: { "content-type": "application/octet-stream" } });
}
