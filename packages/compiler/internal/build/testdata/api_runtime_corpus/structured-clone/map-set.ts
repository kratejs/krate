export async function GET() {
  const k = { id: 1 };
  const m = new Map<any, any>([[k, "obj"], ["s", new Set([1, "1", k])], [NaN, "nan"]]);
  const c = structuredClone(m);
  const keys = [...c.keys()];
  const inner = c.get("s");
  return Response.json({
    size: c.size, isMap: c instanceof Map, sameMap: c === m,
    objKeyCloned: keys[0] !== k && keys[0].id === 1,
    setShared: [...inner][2] === keys[0],
    setValues: [...inner].map((v) => typeof v),
    nan: c.get(NaN),
  });
}
