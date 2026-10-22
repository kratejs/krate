import { Component, useEffect, useState } from "react";
import { log } from "../lib/log";

class Boundary extends Component<{ children?: any }, { message: string }> {
  state = { message: "" };
  static getDerivedStateFromError(e: Error) {
    return { message: e.message };
  }
  componentDidCatch(e: Error) {
    log("didCatch", e.message);
  }
  render() {
    return this.state.message ? <p id="fallback">effect failed: {this.state.message}</p> : this.props.children;
  }
}

function Subscriber(props: { channel: string }) {
  useEffect(() => {
    log("subscribe", props.channel);
    if (props.channel === "bad") throw new Error("cannot subscribe to bad");
    return () => log("unsubscribe", props.channel);
  }, [props.channel]);
  return <p id="channel">{props.channel}</p>;
}

// Unlike event handlers, errors thrown in effects are caught by the nearest
// error boundary.
export default function BoundaryEffectError() {
  const [channel, setChannel] = useState("news");
  return (
    <div>
      <Boundary>
        <Subscriber channel={channel} />
      </Boundary>
      <button id="bad" onClick={() => setChannel("bad")}>
        bad channel
      </button>
    </div>
  );
}
