// Krate style: signals, variable refs on the Stage and on shapes, a keyed
// list of components and a conditional shape, compiled with the page.
import { createSignal, onMount } from "@krate/runtime";
import { Circle, Layer, Rect, Stage } from "@krate/konva";
import type Konva from "konva";

function Dot(props: { x: number }) {
  return <Circle name="dot" x={props.x} y={60} radius={5} fill="black" />;
}

export default function Signals() {
  const [x, setX] = createSignal(10);
  const [items, setItems] = createSignal([1, 2, 3]);
  let stage: Konva.Stage | undefined;
  let rect: Konva.Rect | undefined;
  onMount(() => {
    (window as unknown as { __k: unknown }).__k = { stage: () => stage, rect: () => rect };
  });
  return (
    <main>
      <button id="inc" onClick={() => setX(x() + 10)}>inc</button>
      <button id="rev" onClick={() => setItems([...items()].reverse())}>rev</button>
      <p id="x">{x()}</p>
      <div id="sig">
        <Stage ref={stage} width={200} height={100}>
          <Layer>
            <Rect ref={rect} name="r" x={x()} y={10} width={20} height={20} fill={x() > 15 ? "blue" : "red"} onClick={() => setX(0)} />
            {items().map((i) => (
              <Dot key={i} x={i * 30} />
            ))}
            {x() > 30 && <Rect name="far" x={150} y={10} width={10} height={10} fill="green" />}
          </Layer>
        </Stage>
      </div>
    </main>
  );
}
