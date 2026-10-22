export default function Fixture() {
  return (
    <p>{({ a: { b: 1 } } as any)?.a?.b} {({} as any)?.x?.y ?? "none"}</p>
  );
}
