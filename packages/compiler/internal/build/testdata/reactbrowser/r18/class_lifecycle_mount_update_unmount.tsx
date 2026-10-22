import { Component, useState } from "react";
import { log } from "../lib/log";

class Timer extends Component<{ label: string }, { ticks: number }> {
  state = { ticks: 0 };

  componentDidMount() {
    log("didMount", this.props.label);
  }

  componentDidUpdate(prevProps: { label: string }, prevState: { ticks: number }) {
    log("didUpdate", prevProps.label, "->", this.props.label, "ticks", prevState.ticks, "->", this.state.ticks);
  }

  componentWillUnmount() {
    log("willUnmount", this.props.label);
  }

  render() {
    return (
      <div className="timer">
        <span id="timer-label">{this.props.label}</span>
        <button id="tick" onClick={() => this.setState({ ticks: this.state.ticks + 1 })}>
          ticks {this.state.ticks}
        </button>
      </div>
    );
  }
}

// componentDidMount, componentDidUpdate(prevProps, prevState) and
// componentWillUnmount fire at the right moments with the right arguments.
export default function ClassLifecycleMountUpdateUnmount() {
  const [label, setLabel] = useState("alpha");
  const [show, setShow] = useState(true);
  return (
    <div>
      {show && <Timer label={label} />}
      <button id="relabel" onClick={() => setLabel(label + "+")}>
        relabel
      </button>
      <button id="toggle" onClick={() => setShow(!show)}>
        toggle
      </button>
    </div>
  );
}
