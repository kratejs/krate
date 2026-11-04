export const config = { isr: true, revalidate: 30 };
export default function Isr() {
  return (
    <main>
      <h1 id="page">isr</h1>
      <p id="stamp">{String(globalThis.__krateIsrCounter = (globalThis.__krateIsrCounter || 0) + 1)}</p>
    </main>
  );
}
