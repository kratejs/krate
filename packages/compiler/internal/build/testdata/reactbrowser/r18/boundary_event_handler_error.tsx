import { Component, useState } from "react";
import { log } from "../lib/log";

class Boundary extends Component<{ children?: any }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    log("boundary caught (should not happen)");
    return { failed: true };
  }
  render() {
    return this.state.failed ? <p id="fallback">fallback</p> : this.props.children;
  }
}

function Saver() {
  const [saved, setSaved] = useState(0);
  return (
    <div>
      <p id="saved">saved {saved}</p>
      <button
        id="save"
        onClick={() => {
          setSaved(saved + 1);
          log("saving");
          throw new Error("network down");
        }}
      >
        save
      </button>
    </div>
  );
}

// Errors thrown in event handlers are not caught by error boundaries: they
// surface as uncaught errors and the state update still applies.
export default function BoundaryEventHandlerError() {
  return (
    <Boundary>
      <Saver />
    </Boundary>
  );
}
