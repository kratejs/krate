import { swatches } from "./swatches";
import "./demo.css";

// A plain grid of links. Krate's viewTransitions: "auto" runs each navigation
// inside document.startViewTransition, and elements that share a
// data-view-transition name morph from the tile to the detail page.
export default function Demo() {
  return (
    <ul class="tx-grid">
      {swatches.map((s) => (
        <li>
          <a class="tx-tile" href={"/demos/transitions/" + s.id + "/"}>
            <span
              class="tx-art"
              data-view-transition={"tx-art-" + s.id}
              style={"background: linear-gradient(135deg, " + s.from + ", " + s.to + ")"}
            ></span>
            <span class="tx-name" data-view-transition={"tx-name-" + s.id}>{s.name}</span>
          </a>
        </li>
      ))}
    </ul>
  );
}
