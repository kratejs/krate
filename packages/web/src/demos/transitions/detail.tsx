import { swatches } from "./swatches";
import "./demo.css";

// The detail page reuses the tile's transition names, so the gradient and the
// title grow from the grid into this layout.
export default function Detail(props: { id: string }) {
  const s = swatches.find((x) => x.id === props.id) || swatches[0];
  return (
    <article class="tx-detail">
      <div
        class="tx-hero"
        data-view-transition={"tx-art-" + s.id}
        style={"background: linear-gradient(135deg, " + s.from + ", " + s.to + ")"}
      ></div>
      <h2 data-view-transition={"tx-name-" + s.id}>{s.name}</h2>
      <p>{s.note}</p>
      <a class="tx-back" href="/demos/transitions/">Back to the grid</a>
    </article>
  );
}
