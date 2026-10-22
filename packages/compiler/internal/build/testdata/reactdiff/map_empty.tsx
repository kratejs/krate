export default function Fixture() {
  return (
    <ul>{[].map((x) => <li key={x}>{x}</li>)}</ul>
  );
}
