// A runtime-tier TSX component: it renders per request on the SSR sidecar.
// Kept deliberately compute-light so the benchmark measures the render/transport
// path rather than raw JS throughput.
export default function Widget(props: { label: string }) {
  const items = Array.from({ length: 50 }, (_, i) => `row-${i}`);
  return (
    <section class="widget">
      <h2>{props.label}</h2>
      <ul>
        {items.map((it) => (
          <li>{it}</li>
        ))}
      </ul>
    </section>
  );
}
