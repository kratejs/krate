import { Component, useState } from "react";

type Props = { label?: string; tone?: string; count: number };

class Pill extends Component<Props> {
  static defaultProps = { label: "item", tone: "neutral" };

  render() {
    return (
      <span className={"pill " + this.props.tone}>
        {this.props.count} {this.props.label}
      </span>
    );
  }
}

// static defaultProps fill in props that are missing or undefined, but not
// props that are null.
export default function ClassDefaultProps() {
  const [count, setCount] = useState(1);
  const [tone, setTone] = useState<string | undefined>(undefined);
  return (
    <div>
      <p id="a">
        <Pill count={count} />
      </p>
      <p id="b">
        <Pill count={count} label="messages" tone={tone} />
      </p>
      <button id="inc" onClick={() => setCount(count + 1)}>
        inc
      </button>
      <button id="warn" onClick={() => setTone("warning")}>
        warn
      </button>
      <button id="unset" onClick={() => setTone(undefined)}>
        unset
      </button>
    </div>
  );
}
