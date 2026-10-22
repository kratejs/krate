export default function Fixture() {
  return (
    <ul>
      {["a", "b"].map((x, i) => (
        <li key={i}>{i}:{x}</li>
      ))}
    </ul>
  );
}
