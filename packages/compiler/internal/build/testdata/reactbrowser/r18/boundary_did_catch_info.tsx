import { Component, useState } from "react";
import { log } from "../lib/log";

class Reporter extends Component<{ children?: any }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: { componentStack?: string | null }) {
    log("didCatch", error.message, "has stack", typeof info.componentStack === "string");
  }

  render() {
    return this.state.failed ? <p id="fallback">reported</p> : this.props.children;
  }
}

function Chart(props: { points: number[] }) {
  const max = Math.max(...props.points);
  if (!Number.isFinite(max)) throw new Error("empty chart");
  return <p id="chart">max {max}</p>;
}

// componentDidCatch receives the error and an info object with a component
// stack, after getDerivedStateFromError has switched to the fallback.
export default function BoundaryDidCatchInfo() {
  const [points, setPoints] = useState([1, 5, 3]);
  return (
    <div>
      <Reporter>
        <Chart points={points} />
      </Reporter>
      <button id="clear" onClick={() => setPoints([])}>
        clear data
      </button>
    </div>
  );
}
