export async function GET() {
  const f = new File(["abc"], "dir/name.txt", { type: "Text/Plain", lastModified: 1700000000000 });
  const d = new File([], "x");
  return Response.json({
    name: f.name, type: f.type, size: f.size, lastModified: f.lastModified, text: await f.text(), isBlob: f instanceof Blob, isFile: f instanceof File,
    defaultLM: typeof d.lastModified === "number" && Math.abs(d.lastModified - Date.now()) < 60000, sliceIsFile: f.slice(0, 1) instanceof File,
    numName: new File([], 5 as any).name,
  });
}
