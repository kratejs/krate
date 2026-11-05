import { useState } from 'react';
function Bomb({ armed }) {
  if (armed) throw new Error('broken page exploded');
  return <p id="broken-ok">broken page ok</p>;
}
export default function Broken() {
  const [armed, setArmed] = useState(false);
  return (
    <div id="broken">
      <button id="arm-broken" onClick={() => setArmed(true)}>Arm</button>
      <Bomb armed={armed} />
    </div>
  );
}
