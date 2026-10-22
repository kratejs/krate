export default function Fixture() {
  return (
    <ul>
      {["a", "b", "c"].map((x) => (
        <li key={x}>{x}</li>
      ))}
    </ul>
  );
}
