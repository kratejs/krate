function Inner({ n }: { n: number }) {
  return <i>{n}</i>;
}

function Outer({ n }: { n: number }) {
  return (
    <b>
      <Inner n={n} />
      <Inner n={n + 1} />
    </b>
  );
}

export default function Fixture() {
  return <Outer n={1} />;
}
