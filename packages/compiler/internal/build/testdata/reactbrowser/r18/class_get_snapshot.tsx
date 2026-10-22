import { Component, createRef } from "react";
import { log } from "../lib/log";

type Props = {};
type State = { messages: string[] };

// getSnapshotBeforeUpdate reads the DOM before the update is applied and its
// return value is passed to componentDidUpdate as the third argument.
export default class ClassGetSnapshot extends Component<Props, State> {
  state: State = { messages: ["hello"] };
  listRef = createRef<HTMLUListElement>();

  getSnapshotBeforeUpdate(_prevProps: Props, prevState: State) {
    const rows = this.listRef.current ? this.listRef.current.children.length : -1;
    log("snapshot", "prev messages", prevState.messages.length, "rows in DOM", rows);
    return rows;
  }

  componentDidUpdate(_prevProps: Props, _prevState: State, snapshot: number) {
    log("didUpdate snapshot", snapshot, "rows now", this.listRef.current!.children.length);
  }

  render() {
    return (
      <div>
        <ul ref={this.listRef}>
          {this.state.messages.map((m, i) => (
            <li key={i}>{m}</li>
          ))}
        </ul>
        <button id="send" onClick={() => this.setState({ messages: [...this.state.messages, "msg"] })}>
          send
        </button>
      </div>
    );
  }
}
