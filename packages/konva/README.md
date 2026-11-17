# @krate/konva

A [Konva](https://konvajs.org) 2D canvas renderer for
[Krate](https://krate.js.org), with the API of
[react-konva](https://github.com/konvajs/react-konva): `<Stage>`, `<Layer>`,
every Konva shape as an element, `on*` event props, refs to Konva nodes and
`<Transformer>`. No React ships to the page.

```sh
pnpm add @krate/konva konva
```

```tsx
import { createSignal } from '@krate/runtime';
import { Layer, Rect, Stage, Transformer } from '@krate/konva';
import type Konva from 'konva';

export default function Editor() {
  const [x, setX] = createSignal(20);
  const [hover, setHover] = createSignal(false);
  let rect: Konva.Rect | undefined;
  let tr: Konva.Transformer | undefined;
  return (
    <Stage width={400} height={300}>
      <Layer>
        <Rect
          ref={rect}
          x={x()}
          y={20}
          width={80}
          height={50}
          fill={hover() ? 'tomato' : 'steelblue'}
          draggable
          onMouseEnter={() => setHover(true)}
          onMouseLeave={() => setHover(false)}
          onDragEnd={(e) => setX(e.target.x())}
          onClick={() => tr?.nodes([rect!])}
        />
        <Transformer ref={tr} keepRatio={false} />
      </Layer>
    </Stage>
  );
}
```

Props follow react-konva's rules: attributes are set with `setAttrs` (a
removed prop is set to `undefined`), `on*` props are bound as Konva events
in the `.react-konva-event` namespace after the attributes and rebound only
when the handler changes, and a node moved by drag keeps its position until
a prop changes it (`useStrictMode(true)` resets it on every update).

In a `"use client"` component (a Preact island) the build resolves
`@krate/konva` to `@krate/konva/island`, the same API rendered by Preact, so
a react-konva module runs with `react-konva` imports changed to
`@krate/konva`.

The engine imports Konva's core and its shapes; the filters are not pulled
in unless the app imports them.

Code ported from react-konva is used under the MIT License, see NOTICE.
