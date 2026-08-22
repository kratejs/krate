export default function Index() {
  const items = Array.from({ length: 100 }, (_, i) => i);

  return (
    <main>
      <h1>Benchmark</h1>
      <p>Remix page</p>
      <ul>
        {items.map((i) => (
          <li key={i}>Item {i}</li>
        ))}
      </ul>
    </main>
  );
}
