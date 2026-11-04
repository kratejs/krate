import { useState } from "react";

function Badge(props: { count: number }) {
  return <span id="badge">c:{props.count}</span>;
}

export default function BadgeInCond() {
  const [count, setCount] = useState(0);
  const [show, setShow] = useState(true);
  return (
    <div>
      <button id="inc" onClick={() => setCount(count + 1)}>inc</button>
      <button id="tog" onClick={() => setShow(!show)}>tog</button>
      {show ? <Badge count={count} /> : <em>hidden</em>}
    </div>
  );
}
