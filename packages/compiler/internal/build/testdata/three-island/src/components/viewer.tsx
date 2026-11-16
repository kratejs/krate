'use client';
// A "use client" viewer on a server-rendered page: it runs as a Preact
// island, so @krate/three and @krate/three/drei resolve to their island
// builds. Badge is a React package component sharing the island runtime.
import { Canvas } from "@krate/three";
import { Badge } from "fake-lib";
import * as React from "react";
import { Scene } from "./scene";

class Boundary extends React.Component<{ children: React.ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? <p id="failed">failed</p> : this.props.children;
  }
}

export default function Viewer({ url }: { url: string }) {
  const [near, setNear] = React.useState(1);
  const [paused, setPaused] = React.useState(false);
  const [picked, setPicked] = React.useState("");
  return (
    <div>
      <Badge label="viewer" />
      <button id="near" onClick={() => setNear(near + 1)}>near {near}</button>
      <button id="pause" onClick={() => setPaused(!paused)}>{paused ? "resume" : "pause"}</button>
      <span id="picked">{picked}</span>
      <div id="scene" style={{ width: "400px", height: "300px" }}>
        <Boundary>
          <Canvas
            frameloop={paused ? "never" : "demand"}
            camera={{ fov: 35, position: [4, 2, 4] }}
            gl={{ antialias: true, alpha: true }}
            onCreated={(s) => ((window as any).__three = s)}
            className="viewer"
            aria-label="3D view"
          >
            <Scene url={url} near={near} onPick={setPicked} />
          </Canvas>
        </Boundary>
      </div>
    </div>
  );
}
