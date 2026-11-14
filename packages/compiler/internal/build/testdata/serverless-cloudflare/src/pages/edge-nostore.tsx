export const config = { ssr: true };

export default function Edge() {
  return <main><p id="n">{String((globalThis as any).__n = ((globalThis as any).__n || 0) + 1)}</p></main>;
}
