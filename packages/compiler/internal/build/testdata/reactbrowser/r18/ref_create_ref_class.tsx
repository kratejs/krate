import { Component, createRef } from "react";
import { log } from "../lib/log";

// createRef in a class component: the ref is attached before
// componentDidMount and can be used by instance methods.
export default class RefCreateRefClass extends Component<{}, { copied: string }> {
  state = { copied: "" };
  source = createRef<HTMLInputElement>();

  componentDidMount() {
    log("didMount ref attached", this.source.current !== null, this.source.current?.value);
  }

  copy = () => {
    this.setState({ copied: this.source.current!.value });
  };

  render() {
    return (
      <div>
        <input id="source" ref={this.source} defaultValue="token-123" />
        <button id="copy" onClick={this.copy}>
          copy
        </button>
        <p id="copied">{this.state.copied}</p>
      </div>
    );
  }
}
