import { Component } from "react";
import { log } from "../lib/log";

// Functional setState updates queue on top of each other; object updates
// computed from this.state in the same handler do not.
export default class ClassSetStateFunctional extends Component<{}, { n: number }> {
  state = { n: 0 };

  addThree = () => {
    this.setState((s) => ({ n: s.n + 1 }));
    this.setState((s) => ({ n: s.n + 1 }));
    this.setState((s) => ({ n: s.n + 1 }));
    log("this.state.n in handler", this.state.n);
  };

  addStale = () => {
    this.setState({ n: this.state.n + 1 });
    this.setState({ n: this.state.n + 1 });
  };

  componentDidUpdate(_: {}, prev: { n: number }) {
    log("didUpdate", prev.n, "->", this.state.n);
  }

  render() {
    return (
      <div>
        <p id="n">{this.state.n}</p>
        <button id="three" onClick={this.addThree}>
          +3
        </button>
        <button id="stale" onClick={this.addStale}>
          +1
        </button>
      </div>
    );
  }
}
