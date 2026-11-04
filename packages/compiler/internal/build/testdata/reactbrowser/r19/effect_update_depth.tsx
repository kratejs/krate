import { Component, useLayoutEffect, useState } from "react";
import { log } from "../lib/log";

class Boundary extends Component<{ children?: any }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidCatch(e: Error) {
    log("caught", /Maximum update depth exceeded|#185/.test(e.message) ? "update depth" : e.message);
  }
  render() {
    return this.state.failed ? <p id="fallback">update loop stopped</p> : this.props.children;
  }
}

// A layout effect that keeps writing its own dependency never settles. React
// stops after 50 nested updates with "Maximum update depth exceeded" and the
// nearest error boundary renders its fallback; the page stays responsive.
function Looper() {
  const [go, setGo] = useState(false);
  const [n, setN] = useState(0);
  useLayoutEffect(() => {
    if (go) setN(n + 1);
  }, [go, n]);
  return (
    <div>
      <button id="start" onClick={() => setGo(true)}>
        start
      </button>
      <p id="state">{go ? "looping" : "idle"}</p>
    </div>
  );
}

export default function EffectUpdateDepth() {
  return (
    <Boundary>
      <Looper />
    </Boundary>
  );
}
