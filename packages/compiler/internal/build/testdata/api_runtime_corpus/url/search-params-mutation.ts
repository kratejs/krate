export async function GET() {
  const u = new URL("http://x.example/p?a=1&b=2#h");
  const steps: string[] = [];
  u.searchParams.append("c", "3 4"); steps.push(u.href);
  u.searchParams.set("a", "x&y"); steps.push(u.href);
  u.searchParams.delete("b"); steps.push(u.href);
  u.searchParams.sort(); steps.push(u.href);
  u.search = "?z=9"; steps.push(u.href + " get=" + u.searchParams.get("z"));
  u.searchParams.delete("z"); steps.push(u.href + " search=" + JSON.stringify(u.search));
  u.search = ""; steps.push(u.href);
  u.href = "http://y.example/?k=v"; steps.push("k=" + u.searchParams.get("k"));
  const same = u.searchParams === u.searchParams;
  return Response.json({ steps, same });
}
