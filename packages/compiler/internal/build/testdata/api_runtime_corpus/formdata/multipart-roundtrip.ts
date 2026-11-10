export async function GET() {
  const f = new FormData();
  f.append("text", "héllo\nworld");
  f.append("quote", 'a"b');
  f.append("file", new File([new Uint8Array([0, 1, 2, 255])], "bin.dat", { type: "application/x-bin" }));
  f.append("empty", new Blob([]), "e.txt");
  const r = new Response(f);
  const back = await r.formData();
  const out: unknown[] = [];
  for (const [k, v] of back) out.push(typeof v === "string" ? [k, v] : [k, v.name, v.type, v.size, Array.from(new Uint8Array(await v.arrayBuffer()))]);
  return Response.json(out);
}
