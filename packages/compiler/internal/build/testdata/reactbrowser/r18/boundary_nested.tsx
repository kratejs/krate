import { Component, useState } from "react";
import { log } from "../lib/log";

class Catch extends Component<{ name: string; children?: any }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidCatch(e: Error) {
    log(this.props.name, "caught", e.message);
  }
  render() {
    return this.state.failed ? <p className="fallback">{this.props.name} fallback</p> : this.props.children;
  }
}

function Bomb(props: { armed: boolean; label: string }) {
  if (props.armed) throw new Error(props.label + " exploded");
  return <span className="bomb">{props.label}</span>;
}

// The nearest boundary catches; the outer boundary and its other children
// stay mounted.
export default function BoundaryNested() {
  const [inner, setInner] = useState(false);
  const [outer, setOuter] = useState(false);
  return (
    <div>
      <Catch name="outer">
        <Bomb armed={outer} label="outer-bomb" />
        <Catch name="inner">
          <Bomb armed={inner} label="inner-bomb" />
        </Catch>
      </Catch>
      <button id="inner" onClick={() => setInner(true)}>
        inner
      </button>
      <button id="outer" onClick={() => setOuter(true)}>
        outer
      </button>
    </div>
  );
}
