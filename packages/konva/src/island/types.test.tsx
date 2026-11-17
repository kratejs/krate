/** @jsxImportSource preact */
// Type-checked only (tsc): the react-konva patterns of a migrated "use client"
// editor, written against the island build under Preact's JSX.

import type Konva from 'konva';
import type { KonvaEventObject } from 'konva/lib/Node';
import { useEffect, useRef, useState } from 'preact/hooks';
import { Circle, Group, Layer as KLayer, Line, Path, Rect, Stage, Text, Transformer, useContextBridge } from './index.js';

function Handles(props: { k: number; onMove(x: number): void }) {
  const cursor = (e: KonvaEventObject<MouseEvent>, c: string) => {
    const el = e.target.getStage()?.container();
    if (el) el.style.cursor = c;
  };
  return (
    <Group name="fx-handles" x={1} y={2}>
      <Line points={[0, 0, 1, 1]} stroke="#fff" strokeWidth={1 * props.k} dash={[4, 3]} listening={false} />
      <Circle
        radius={5}
        hitStrokeWidth={8}
        draggable
        onPointerDown={(e: KonvaEventObject<MouseEvent>) => {
          e.cancelBubble = true;
        }}
        onMouseEnter={(e) => cursor(e, 'move')}
        onMouseLeave={(e) => cursor(e, 'default')}
        onDragMove={(e) => props.onMove(e.target.x())}
      />
      <Path data="M 0 0 L 1 1" stroke="#000" lineJoin="round" />
    </Group>
  );
}

export function Overlay(props: { feel: boolean; items: Array<{ id: string; x: number }> }) {
  const stageRef = useRef<Konva.Stage>(null);
  const trRef = useRef<Konva.Transformer>(null);
  const [marquee, setMarquee] = useState<{ x: number; y: number } | null>(null);
  const Bridge = useContextBridge();
  useEffect(() => {
    const tr = trRef.current;
    const stage = stageRef.current;
    if (!tr || !stage) return;
    tr.nodes(props.items.map((i) => stage.findOne(`#item-${i.id}`)).filter((n): n is Konva.Node => !!n));
    tr.getLayer()?.batchDraw();
  }, [props.items]);
  const stageDown = (e: KonvaEventObject<MouseEvent>) => {
    if (e.evt.button !== 0 || e.target !== e.target.getStage()) return;
    const p = e.target.getStage()!.getRelativePointerPosition();
    if (p) setMarquee(p);
  };
  const onEmpty = (e: KonvaEventObject<PointerEvent>) => void e.target.getStage()?.getRelativePointerPosition();
  return (
    <Stage
      ref={stageRef}
      width={100}
      height={100}
      scaleX={2}
      style={{ cursor: props.feel ? 'grab' : 'default' }}
      {...(props.feel ? { onPointerDown: stageDown, onPointerMove: onEmpty } : { onMouseDown: stageDown })}
    >
      <Bridge>
        <KLayer>
          {props.items.map((it) => (
            <Rect
              key={it.id}
              id={`item-${it.id}`}
              x={it.x}
              shadowColor="#000"
              shadowBlur={3}
              dash={props.feel ? [6, 4] : undefined}
              draggable
              {...(props.feel ? { onDblClick: () => {}, onDblTap: () => {} } : {})}
              onDragStart={(e) => void e.target.x()}
              onDragEnd={() => {}}
              onTransformStart={(e) => void e.target.rotation()}
              onTransform={(e) => {
                const n = e.target;
                n.scaleX(1);
                void trRef.current?.getActiveAnchor();
              }}
              onTransformEnd={() => {}}
            />
          ))}
          {marquee ? <Rect x={marquee.x} y={marquee.y} listening={false} /> : null}
          <Handles k={1} onMove={() => {}} />
          <Transformer
            ref={trRef}
            rotateEnabled
            keepRatio={false}
            ignoreStroke
            flipEnabled={false}
            anchorSize={9}
            rotationSnaps={[0, 90]}
            boundBoxFunc={(oldBox, newBox) => (Math.abs(newBox.width) < 4 ? oldBox : newBox)}
          />
          <Text text="label" fontSize={12} fontStyle="bold" fill="#fff" listening={false} />
        </KLayer>
      </Bridge>
    </Stage>
  );
}
