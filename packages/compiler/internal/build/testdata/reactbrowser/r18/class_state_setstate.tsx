import { Component } from "react";

type State = { count: number; label: string };

// Basic class component state: setState merges partial state.
export default class ClassStateSetState extends Component<{}, State> {
  state: State = { count: 0, label: "clicks" };

  increment = () => {
    this.setState({ count: this.state.count + 1 });
  };

  rename = () => {
    this.setState({ label: "taps" });
  };

  render() {
    return (
      <div>
        <p id="out">
          {this.state.count} {this.state.label}
        </p>
        <button id="inc" onClick={this.increment}>
          +1
        </button>
        <button id="rename" onClick={this.rename}>
          rename
        </button>
      </div>
    );
  }
}
