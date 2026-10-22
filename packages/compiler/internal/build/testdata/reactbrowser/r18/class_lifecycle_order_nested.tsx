import { Component, useEffect, useState } from "react";
import { log } from "../lib/log";

class Panel extends Component<{ name: string; children?: any; rev: number }> {
  componentDidMount() {
    log("didMount", this.props.name);
  }
  componentDidUpdate() {
    log("didUpdate", this.props.name);
  }
  componentWillUnmount() {
    log("willUnmount", this.props.name);
  }
  render() {
    return (
      <section data-name={this.props.name}>
        {this.props.name} r{this.props.rev}
        {this.props.children}
      </section>
    );
  }
}

function HookChild(props: { rev: number }) {
  useEffect(() => {
    log("hook effect", props.rev);
    return () => log("hook cleanup", props.rev);
  }, [props.rev]);
  return <i>hook {props.rev}</i>;
}

// Nested class lifecycles mixed with hooks: children mount and update before
// parents; unmount runs parent first.
export default function ClassLifecycleOrderNested() {
  const [rev, setRev] = useState(0);
  const [show, setShow] = useState(true);
  return (
    <div>
      {show && (
        <Panel name="outer" rev={rev}>
          <Panel name="inner" rev={rev}>
            <HookChild rev={rev} />
          </Panel>
          <Panel name="sibling" rev={rev} />
        </Panel>
      )}
      <button id="rev" onClick={() => setRev(rev + 1)}>
        rev
      </button>
      <button id="hide" onClick={() => setShow(false)}>
        hide
      </button>
    </div>
  );
}
