function Show({ s }: { s: string }) {
  return <p title={s}>{s}</p>;
}

export default function Fixture() {
  return <Show s="a < b > c &amp; d" />;
}
