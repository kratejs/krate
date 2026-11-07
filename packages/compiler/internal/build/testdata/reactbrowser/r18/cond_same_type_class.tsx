import { Component, useState } from "react";
import { log } from "../lib/log";

class Counter extends Component<{ label: string }, { n: number }> {
  state = { n: 0 };
  componentDidMount() {
    log("didMount", this.props.label);
  }
  componentWillUnmount() {
    log("willUnmount", this.props.label);
  }
  render() {
    return (
      <button className="c" onClick={() => this.setState({ n: this.state.n + 1 })}>
        {this.props.label}:{this.state.n}
      </button>
    );
  }
}

// A class component keeps its instance across the flip too.
export default function CondSameTypeClass() {
  const [alt, setAlt] = useState(false);
  return (
    <div>
      <button id="flip" onClick={() => setAlt((v) => !v)}>flip {String(alt)}</button>
      {alt ? <Counter label="one" /> : <Counter label="two" />}
    </div>
  );
}
