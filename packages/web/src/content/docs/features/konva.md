---
title: 2D canvas with Konva
description: Draw interactive 2D canvases with @krate/konva, which has the react-konva API (Stage, Layer, shapes, events, Transformer) and ships no React.
order: 20
---

# 2D canvas with Konva

`@krate/konva` renders [Konva](https://konvajs.org) stages from JSX: a
`<Stage>`, `<Layer>` and every Konva shape as an element, `on*` event props,
refs to Konva nodes, drag and drop and the `<Transformer>`. It has the API of
[react-konva](https://konvajs.org/docs/react/), so code written for
react-konva runs with the import changed from `react-konva` to
`@krate/konva`, and no React ships to the page.

```sh
pnpm add @krate/konva konva
```

```tsx
import { createSignal } from "@krate/runtime";
import { Layer, Rect, Stage, Transformer } from "@krate/konva";
import type Konva from "konva";

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
          fill={hover() ? "tomato" : "steelblue"}
          draggable
          onMouseEnter={() => setHover(true)}
          onMouseLeave={() => setHover(false)}
          onDragEnd={(e) => setX(e.target.x())}
          onClick={() => tr?.nodes([rect!])}
        />
        <Transformer ref={tr} keepRatio rotateEnabled anchorSize={8} />
      </Layer>
    </Stage>
  );
}
```

The same editor in React style, as react-konva writes it, works unchanged:

```tsx
import { useEffect, useRef, useState } from "react";
import { Layer, Rect, Stage, Transformer } from "@krate/konva";
import type Konva from "konva";

export default function Editor() {
  const [selected, setSelected] = useState(false);
  const rectRef = useRef<Konva.Rect>(null);
  const trRef = useRef<Konva.Transformer>(null);
  useEffect(() => {
    trRef.current?.nodes(selected && rectRef.current ? [rectRef.current] : []);
  }, [selected]);
  return (
    <Stage width={400} height={300} onClick={(e) => e.target === e.target.getStage() && setSelected(false)}>
      <Layer>
        <Rect ref={rectRef} x={20} y={20} width={80} height={50} fill="steelblue" draggable onClick={() => setSelected(true)} />
        <Transformer ref={trRef} boundBoxFunc={(oldBox, box) => (box.width < 10 ? oldBox : box)} />
      </Layer>
    </Stage>
  );
}
```

## How it works

`<Stage>` is a renderer component (marked `@renderer`, like `@krate/three`'s
`<Canvas>`). On the server it renders only its container `<div>`; in the
browser it creates a `Konva.Stage` in that div and builds its children as
Konva nodes. The children are compiled with the page: a prop that reads a
signal (or state) is applied to the same node when it changes, a condition
adds or removes nodes, and a keyed `.map()` keeps, moves and removes nodes by
key, so a drag or a selection survives a re-render. Components inside the
stage run once with their own owner, like any Krate component.

The elements are the Konva class names (`Rect` is the string `"Rect"`, as in
react-konva). The engine is bundled with the page like an npm dependency, so
the app and the engine share one copy of `konva`; it imports Konva's core and
its shapes; Konva's filters load when the app imports `konva` itself to use
them.

## Stage

`<Stage>` takes the `Konva.Stage` config (`width`, `height`, `scaleX`,
`draggable`, ...) and its events, plus the attributes of its container div:
`id`, `className` (or `class`), `style`, `role`, `tabIndex`, `title` and
`accessKey`. Its `ref` receives the `Konva.Stage`.

## Shapes and props

`Layer`, `FastLayer`, `Group`, `Label`, `Rect`, `Circle`, `Ellipse`,
`Wedge`, `Line`, `Sprite`, `Image`, `Text`, `TextPath`, `Star`, `Ring`,
`Arc`, `Tag`, `Path`, `RegularPolygon`, `Arrow`, `Shape` and `Transformer`
are available. Props follow react-konva's rules:

- props are Konva attributes, set with `setAttrs` (`listening`, `dash`,
  `hitStrokeWidth`, `shadowBlur`, `dragBoundFunc`, `sceneFunc`, ...); a prop
  that is no longer passed is set to `undefined`;
- a node moved by drag or by the Transformer keeps its position until a prop
  changes it. `useStrictMode(true)` (or `_useStrictMode` on one node)
  compares props with the node's attributes on every update instead, which
  resets such a node to its props;
- `zIndex` is not used for ordering (a warning says so): the order of the
  JSX is the order of the nodes.

`<Shape sceneFunc={(ctx, shape) => { ... }} />` draws custom shapes;
`hitFunc` draws its hit region.

## Events

Every `on*` prop is a Konva event: `onClick`, `onDblClick`, `onTap`,
`onDblTap`, `onMouseEnter`, `onMouseLeave`, `onMouseMove`, `onMouseDown`,
`onMouseUp`, `onPointerDown`, `onPointerMove`, `onPointerUp`, `onWheel`,
`onDragStart`, `onDragMove`, `onDragEnd`, `onTransformStart`, `onTransform`,
`onTransformEnd` and the rest. The handler receives a `KonvaEventObject`
(`e.target`, `e.evt`, `e.cancelBubble`), and events bubble from a shape to
its group, layer and stage, so `e.target === e.target.getStage()` tells a
click on empty space. Handlers are bound in the `.react-konva-event`
namespace after the attributes, and rebound only when the handler changes.

## Refs

A `ref` receives the Konva node: a Krate variable (`let rect; <Rect
ref={rect} />`), a `useRef` object or a callback ref. The ref is set once
the node's children are built, so a parent's `onMount` or layout effect sees
it, and released when the node is removed.

## Transformer

`<Transformer>` takes Konva's Transformer config (`keepRatio`,
`rotateEnabled`, `enabledAnchors`, `anchorSize`, `ignoreStroke`,
`boundBoxFunc`, ...). Attach it to nodes through its ref, as with
react-konva: `tr.nodes([node])` in an effect, an event handler or
`onMount`. `onTransformStart`, `onTransform` and `onTransformEnd` fire on the
transformed nodes and on the Transformer.

## Context

A context provider around the `<Stage>` reaches the components inside it, as
react-konva's bridge does: a `useContext` read in a shape component sees the
value of the provider around the stage.
`useContextBridge()` is provided for code that wraps the stage children in
its bridge; it renders its children as they are.

## In "use client" components

In a `"use client"` component of a server-rendered page (a Preact island),
the build resolves `@krate/konva` to `@krate/konva/island`, the same API
rendered by Preact: the island's elements become Konva nodes through a small
DOM-like host, its hooks are Preact's, and its refs, keys and context behave
as in React. The page loads the island runtime it already shares with the
other islands and no React.

If the module is part of a real-React island for another reason (a library
built on `react-reconciler` that `@krate/konva` cannot replace), the build
runs `@krate/konva` there as `react-konva`, so keep `react-konva` installed
in that case.

## Migrating from react-konva

`krate migrate nextjs` points `react-konva` imports at `@krate/konva` when
every react-konva API the project's modules use is provided, and adds
`@krate/konva` to `package.json` (a manual item without `--update-package`).
Type imports from `konva` itself (`import type Konva from "konva"`,
`KonvaEventObject` from `konva/lib/Node`) are left as they are, and `konva`
stays a dependency. One unsupported API (`KonvaRenderer`, an internal
react-konva subpath, a package built on react-konva such as
`react-konva-utils`) keeps every react-konva module as it is, because they
render each other's shapes in one stage, and each blocking use is listed as
a manual item with the file and the API.

## Differences from react-konva

- There is no React reconciler: `KonvaRenderer` is not exported.
- Text children (`<Group>hello</Group>`) are not drawn; react-konva logs an
  error for them too. Use `<Text text="hello" />`.
- In a Krate page, a spread prop (`<Rect {...props} />`) is read once; write
  the reactive props out.
- Konva redraws a layer on the next animation frame after a change, as with
  react-konva; `eventBatchFunc` is passed to the stage as it is.
- In a Preact island, Suspense around shapes follows Preact's compat
  Suspense; React's `Activity` is not used.
