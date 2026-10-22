function Pair() {
  return (
    <>
      <dt>k</dt>
      <dd>v</dd>
    </>
  );
}

export default function Fixture() {
  return (
    <dl>
      <Pair />
      <Pair />
    </dl>
  );
}
