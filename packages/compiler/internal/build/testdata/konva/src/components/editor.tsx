"use client";
// A canvas editor written against react-konva, as `krate migrate nextjs`
// leaves it with the import pointed at @krate/konva. The same module is
// compiled into a Krate page (pages/index.tsx) and runs as a Preact island on
// a server-rendered page (pages/island.tsx).
import type Konva from "konva";
import type { KonvaEventObject } from "konva/lib/Node";
import { createContext, useContext, useEffect, useRef, useState } from "react";
import { Circle, Group, Layer as KLayer, Line, Rect, Shape, Stage, Text, Transformer } from "@krate/konva";

const Theme = createContext("plain");

function log(entry: string) {
  const w = window as unknown as { __log?: string[] };
  (w.__log ??= []).push(entry);
}

function Themed() {
  const theme = useContext(Theme);
  return <Text name="themed" x={300} y={10} text={"theme:" + theme} fontSize={12} />;
}

export default function Editor() {
  const stageRef = useRef<Konva.Stage>(null);
  const trRef = useRef<Konva.Transformer>(null);
  const boxRef = useRef<Konva.Rect>(null);
  const [fill, setFill] = useState("red");
  const [boxX, setBoxX] = useState(40);
  const [size, setSize] = useState({ w: 80, h: 60 });
  const [selected, setSelected] = useState(false);
  const [hover, setHover] = useState(false);
  const [clicks, setClicks] = useState(0);
  const [items, setItems] = useState(["a", "b", "c"]);
  const [showBall, setShowBall] = useState(true);
  const [editing, setEditing] = useState(false);

  useEffect(() => {
    (window as unknown as { __stage: unknown }).__stage = stageRef.current;
  }, []);
  useEffect(() => {
    const tr = trRef.current;
    if (!tr) return;
    tr.nodes(selected && boxRef.current ? [boxRef.current] : []);
    tr.getLayer()?.batchDraw();
  }, [selected]);

  const onStageClick = (e: KonvaEventObject<MouseEvent>) => {
    if (e.target !== e.target.getStage()) return;
    setSelected(false);
    log("stage");
  };

  return (
    <div>
      <button id="recolor" onClick={() => setFill(fill === "red" ? "blue" : "red")}>recolor</button>
      <button id="move" onClick={() => setBoxX(boxX + 10)}>move</button>
      <button id="reorder" onClick={() => setItems([...items].reverse())}>reorder</button>
      <button id="drop" onClick={() => setItems(items.slice(1))}>drop</button>
      <button id="toggle" onClick={() => setShowBall(!showBall)}>toggle</button>
      <p id="status">{`clicks:${clicks} hover:${hover} selected:${selected} box:${Math.round(boxX)} size:${Math.round(size.w)}x${Math.round(size.h)} editing:${editing}`}</p>
      <Theme.Provider value="dark">
        <div id="editor" style={{ width: "600px", height: "400px" }}>
          <Stage ref={stageRef} width={600} height={400} onClick={onStageClick}>
            <KLayer>
              <Rect name="bg" x={0} y={0} width={600} height={400} fill="#eeeeee" listening={false} />
              <Rect
                ref={boxRef}
                name="box"
                x={boxX}
                y={40}
                width={size.w}
                height={size.h}
                fill={fill}
                stroke="black"
                strokeWidth={3}
                strokeScaleEnabled={false}
                draggable
                shadowColor="black"
                shadowBlur={4}
                shadowOffsetX={2}
                onClick={() => {
                  setSelected(true);
                  log("box");
                }}
                onDragEnd={(e) => setBoxX(e.target.x())}
                onTransformEnd={(e) => {
                  const n = e.target;
                  setSize({ w: n.width() * n.scaleX(), h: n.height() * n.scaleY() });
                  n.scaleX(1);
                  n.scaleY(1);
                  log("transformend");
                }}
              />
              {showBall && (
                <Circle
                  name="ball"
                  x={300}
                  y={200}
                  radius={30}
                  fill="green"
                  onMouseEnter={() => setHover(true)}
                  onMouseLeave={() => setHover(false)}
                  onClick={() => setClicks((c) => c + 1)}
                />
              )}
              <Line name="wire" points={[100, 300, 500, 300]} stroke="black" strokeWidth={1} hitStrokeWidth={20} dash={[6, 4]} onClick={() => log("wire")} />
              <Circle
                name="knob"
                x={500}
                y={100}
                radius={10}
                fill="orange"
                draggable
                dragBoundFunc={(pos) => ({ x: pos.x, y: 100 })}
                onDragMove={(e) => log("knob:" + Math.round(e.target.y()))}
              />
              <Group name="list">
                {items.map((id, i) => (
                  <Rect key={id} name={"item-" + id} x={20 + i * 30} y={350} width={20} height={20} fill="purple" />
                ))}
              </Group>
              <Text name="label" x={150} y={200} text={editing ? "editing" : "double-click me"} fontSize={16} onDblClick={() => setEditing(true)} />
              <Shape
                name="custom"
                x={400}
                y={250}
                fill="teal"
                sceneFunc={(ctx, shape) => {
                  ctx.beginPath();
                  ctx.rect(0, 0, 30, 30);
                  ctx.fillStrokeShape(shape);
                }}
                onClick={() => log("custom")}
              />
              <Themed />
              <Transformer
                ref={trRef}
                keepRatio
                rotateEnabled
                anchorSize={10}
                ignoreStroke
                onTransformStart={() => log("transformstart")}
                boundBoxFunc={(oldBox, newBox) => (newBox.width < 20 ? oldBox : newBox)}
              />
            </KLayer>
          </Stage>
        </div>
      </Theme.Provider>
    </div>
  );
}
