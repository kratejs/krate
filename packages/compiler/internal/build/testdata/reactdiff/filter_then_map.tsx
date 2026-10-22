export default function Fixture() {
  return (
    <ul>
      {[1, 2, 3, 4, 5].filter((n) => n % 2 === 1).map((n) => (
        <li key={n}>{n}</li>
      ))}
    </ul>
  );
}
