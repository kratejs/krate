import { useState } from 'react';
function Bomb({ armed }) {
  if (armed) throw new Error('crash page exploded');
  return <p id="crash-ok">crash page ok</p>;
}
export default function Crash() {
  const [armed, setArmed] = useState(false);
  const [n, setN] = useState(0);
  return (
    <div id="crash">
      <button id="crash-count" onClick={() => setN(n + 1)}>{n}</button>
      <button id="arm" onClick={() => setArmed(true)}>Arm</button>
      <Bomb armed={armed} />
    </div>
  );
}
