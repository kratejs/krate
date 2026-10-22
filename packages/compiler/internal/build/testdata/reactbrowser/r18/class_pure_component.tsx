import { PureComponent, useState } from "react";
import { log } from "../lib/log";

class Avatar extends PureComponent<{ name: string; size: number }> {
  render() {
    log("Avatar render", this.props.name, this.props.size);
    return (
      <span className="avatar" style={{ width: this.props.size }}>
        {this.props.name}
      </span>
    );
  }
}

// PureComponent shallowly compares props and skips render when they are
// equal.
export default function ClassPureComponent() {
  const [name, setName] = useState("Ada");
  const [clock, setClock] = useState(0);
  return (
    <div>
      <Avatar name={name} size={32} />
      <p id="clock">{clock}</p>
      <button id="tick" onClick={() => setClock(clock + 1)}>
        tick
      </button>
      <button id="rename" onClick={() => setName("Grace")}>
        rename
      </button>
    </div>
  );
}
