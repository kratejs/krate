import { Component, useState } from "react";
import { log } from "../lib/log";

type Props = { userId: number };
type State = { userId: number; draft: string };

class Editor extends Component<Props, State> {
  state: State = { userId: this.props.userId, draft: "" };

  // Reset the draft whenever a different user is selected.
  static getDerivedStateFromProps(props: Props, state: State) {
    log("gDSFP", props.userId, state.userId);
    if (props.userId !== state.userId) {
      return { userId: props.userId, draft: "" };
    }
    return null;
  }

  render() {
    return (
      <div>
        <p id="user">user {this.state.userId}</p>
        <input id="draft" value={this.state.draft} onChange={(e) => this.setState({ draft: e.target.value })} />
      </div>
    );
  }
}

// getDerivedStateFromProps runs before every render (props or state change)
// and can reset internal state when a prop changes.
export default function ClassDerivedState() {
  const [user, setUser] = useState(1);
  return (
    <div>
      <Editor userId={user} />
      <button id="next" onClick={() => setUser(user + 1)}>
        next user
      </button>
    </div>
  );
}
