import { Component, useState } from "react";
import { log } from "../lib/log";

class Boundary extends Component<{ name: string; children?: any; fallback: (message: string) => any }, { error: string }> {
  state = { error: "" };
  static getDerivedStateFromError(e: Error) {
    return { error: e.message };
  }
  componentDidCatch(e: Error) {
    log(this.props.name, "caught", e.message);
  }
  render() {
    return this.state.error ? this.props.fallback(this.state.error) : this.props.children;
  }
}

function Bomb(props: { armed: boolean }) {
  if (props.armed) throw new Error("page failed");
  return <p id="page">page ok</p>;
}

function BrokenFallback(props: { message: string }) {
  throw new Error("fallback failed after: " + props.message);
}

// An error thrown by a boundary's own fallback is not caught by that boundary:
// it goes to the next boundary up.
export default function BoundaryFallbackThrows() {
  const [armed, setArmed] = useState(false);
  return (
    <div>
      <Boundary name="outer" fallback={(m) => <p id="outer-fallback">{m}</p>}>
        <h1 id="title">segment</h1>
        <Boundary name="inner" fallback={(m) => <BrokenFallback message={m} />}>
          <Bomb armed={armed} />
        </Boundary>
      </Boundary>
      <button id="arm" onClick={() => setArmed(true)}>
        arm
      </button>
    </div>
  );
}
