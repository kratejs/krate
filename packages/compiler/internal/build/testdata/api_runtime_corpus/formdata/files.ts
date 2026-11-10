export async function GET() {
  const f = new FormData();
  f.append("blob", new Blob(["bb"], { type: "text/x" }));
  f.append("named", new Blob(["nn"]), "given.bin");
  f.append("file", new File(["ff"], "orig.txt", { type: "text/plain" }));
  f.append("renamed", new File(["rr"], "orig.txt"), "new.txt");
  const d = (k: string) => { const v = f.get(k) as File; return [v instanceof File, v.name, v.type, v.size]; };
  return Response.json({ blob: d("blob"), named: d("named"), file: d("file"), renamed: d("renamed"), str: typeof f.get("missing") });
}
