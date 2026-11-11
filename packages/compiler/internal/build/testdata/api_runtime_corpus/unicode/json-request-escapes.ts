export async function POST(request) {
  const v = await request.json();
  return Response.json({ v, lens: [v.pair.length, v.lone.length], codes: Array.from(v.lone as string, (c) => c.charCodeAt(0).toString(16)) });
}
