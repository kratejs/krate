import { Component, useState } from "react";
import { log } from "../lib/log";

class Boundary extends Component<{ children?: any; onReset: () => void }, { error: string }> {
  state = { error: "" };

  static getDerivedStateFromError(e: Error) {
    return { error: e.message };
  }

  componentDidMount() {
    log("boundary mounted");
  }

  render() {
    if (this.state.error) {
      return (
        <div id="fallback">
          <p>{this.state.error}</p>
          <button id="retry" onClick={this.props.onReset}>
            retry
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}

function Widget(props: { attempt: number; fail: boolean }) {
  if (props.fail) throw new Error("widget failed on attempt " + props.attempt);
  return <p id="widget">widget ok (attempt {props.attempt})</p>;
}

// Changing the boundary's key remounts it, which resets the error state.
export default function BoundaryResetKey() {
  const [attempt, setAttempt] = useState(0);
  const [fail, setFail] = useState(false);
  return (
    <div>
      <Boundary
        key={attempt}
        onReset={() => {
          setFail(false);
          setAttempt(attempt + 1);
        }}
      >
        <Widget attempt={attempt} fail={fail} />
      </Boundary>
      <button id="fail" onClick={() => setFail(true)}>
        fail
      </button>
    </div>
  );
}
