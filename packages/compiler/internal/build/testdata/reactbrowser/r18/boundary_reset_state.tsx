import { Component, useState } from "react";
import { log } from "../lib/log";

class Boundary extends Component<{ children?: any }, { error: string }> {
  state = { error: "" };
  static getDerivedStateFromError(e: Error) {
    return { error: e.message };
  }
  reset = () => this.setState({ error: "" });
  render() {
    if (this.state.error) {
      return (
        <div id="fallback">
          <p id="msg">{this.state.error}</p>
          <button id="reset" onClick={this.reset}>
            try again
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}

function Counter(props: { armed: boolean }) {
  const [n, setN] = useState(0);
  if (props.armed) throw new Error("armed at " + n);
  return (
    <button id="count" onClick={() => setN(n + 1)}>
      count {n}
    </button>
  );
}

// An error boundary that resets itself with setState({ error: "" }) mounts its
// children again with fresh state; the children's props are the parent's, so
// the parent decides whether they fail again.
export default function BoundaryResetState() {
  const [armed, setArmed] = useState(false);
  return (
    <div>
      <Boundary>
        <Counter armed={armed} />
      </Boundary>
      <button
        id="arm"
        onClick={() => {
          log("arm");
          setArmed(true);
        }}
      >
        arm
      </button>
      <button id="disarm" onClick={() => setArmed(false)}>
        disarm
      </button>
    </div>
  );
}
