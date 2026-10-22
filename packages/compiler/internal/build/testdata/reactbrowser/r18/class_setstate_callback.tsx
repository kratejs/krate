import { Component } from "react";
import { log } from "../lib/log";

// The setState callback runs after the update is committed and sees the new
// state and DOM.
export default class ClassSetStateCallback extends Component<{}, { items: string[] }> {
  state = { items: ["first"] };

  add = () => {
    this.setState(
      (s) => ({ items: [...s.items, "item " + (s.items.length + 1)] }),
      () => {
        log("callback items", this.state.items.length, "rows in DOM", document.querySelectorAll("#rows li").length);
      },
    );
    log("after setState call", this.state.items.length);
  };

  render() {
    return (
      <div>
        <ul id="rows">
          {this.state.items.map((it) => (
            <li key={it}>{it}</li>
          ))}
        </ul>
        <button id="add" onClick={this.add}>
          add
        </button>
      </div>
    );
  }
}
