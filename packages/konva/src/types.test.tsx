// Type-checked only (tsc): react-konva elements in a Krate page, with
// signals, Krate-style variable refs and keyed lists.

import { createSignal } from '@krate/runtime';
import type Konva from 'konva';
import { Circle, Layer, Rect, Stage, Transformer } from './index.js';

export default function Editor() {
  const [items, setItems] = createSignal([{ id: 'a', x: 10 }]);
  const [hover, setHover] = createSignal(false);
  let tr: Konva.Transformer | undefined;
  let stage: Konva.Stage | undefined;
  return (
    <Stage ref={stage} width={400} height={300} class="editor" style={{ cursor: 'default' }}>
      <Layer>
        {items().map((it) => (
          <Rect
            key={it.id}
            id={`item-${it.id}`}
            x={it.x}
            width={20}
            height={20}
            fill={hover() ? 'blue' : 'gray'}
            draggable
            onMouseEnter={() => setHover(true)}
            onMouseLeave={() => setHover(false)}
            onDragEnd={(e) => setItems(items().map((i) => (i.id === it.id ? { ...i, x: e.target.x() } : i)))}
          />
        ))}
        <Circle radius={4} dragBoundFunc={(pos) => ({ x: pos.x, y: 0 })} />
        <Transformer ref={tr} keepRatio={false} boundBoxFunc={(o, n) => (n.width < 4 ? o : n)} />
      </Layer>
    </Stage>
  );
}
