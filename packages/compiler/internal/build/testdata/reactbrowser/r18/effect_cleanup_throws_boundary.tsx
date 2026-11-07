import { Component, useEffect, useState } from "react";
import { log } from "../lib/log";

class Boundary extends Component<{ children?: any }, { err: string | null }> {
  state = { err: null as string | null };
  static getDerivedStateFromError(e: Error) {
    return { err: e.message };
  }
  componentDidCatch(e: Error) {
    log("caught", e.message);
  }
  render() {
    if (this.state.err) {
      return (
        <div id="fb">
          <span>fallback {this.state.err}</span>
          <button id="reset" onClick={() => this.setState({ err: null })}>reset</button>
        </div>
      );
    }
    return this.props.children;
  }
}

function Thrower(props: { n: number }) {
  useEffect(() => {
    log("thrower setup", props.n);
    return () => {
      log("thrower cleanup", props.n);
      if (props.n === 1) throw new Error("cleanup boom");
    };
  }, [props.n]);
  return <span id="thrower">thrower {props.n}</span>;
}

function Sibling(props: { n: number }) {
  useEffect(() => {
    log("sibling setup", props.n);
    return () => log("sibling cleanup", props.n);
  }, [props.n]);
  return <span id="sibling">sibling</span>;
}

export default function EffectCleanupThrowsBoundary() {
  const [n, setN] = useState(1);
  return (
    <div>
      <button id="next" onClick={() => setN(n + 1)}>next {n}</button>
      <Boundary>
        <Thrower n={n} />
      </Boundary>
      <Sibling n={n} />
    </div>
  );
}
