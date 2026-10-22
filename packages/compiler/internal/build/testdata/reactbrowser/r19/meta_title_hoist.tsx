import { useState } from "react";

function Inbox(props: { unread: number }) {
  return (
    <section>
      <title>{"Inbox (" + props.unread + ")"}</title>
      <h1 id="heading">Inbox</h1>
      <p id="unread">{props.unread} unread</p>
    </section>
  );
}

// React 19 document metadata: a <title> rendered anywhere is hoisted into
// <head> and updates with state. Krate's equivalent is <Head>; this fixture
// records how plain React <title> behaves under Krate.
export default function MetaTitleHoist() {
  const [unread, setUnread] = useState(3);
  return (
    <div>
      <Inbox unread={unread} />
      <button id="read" onClick={() => setUnread(Math.max(0, unread - 1))}>
        read one
      </button>
    </div>
  );
}
