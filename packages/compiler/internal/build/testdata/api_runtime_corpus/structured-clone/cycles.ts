export async function GET() {
  const a: any = { name: "a", list: [] };
  a.self = a;
  a.list.push(a, a.list);
  const shared = { s: 1 };
  a.x = shared; a.y = shared;
  const c = structuredClone(a);
  return Response.json({ self: c.self === c, notOrig: c !== a, list0: c.list[0] === c, list1: c.list[1] === c.list, shared: c.x === c.y, sharedNotOrig: c.x !== shared });
}
