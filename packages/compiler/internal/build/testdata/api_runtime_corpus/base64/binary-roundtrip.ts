export async function GET() {
  let s = "";
  for (let i = 0; i < 256; i++) s += String.fromCharCode(i);
  const enc = btoa(s);
  const dec = atob(enc);
  return Response.json({ enc, same: dec === s, len: dec.length });
}
