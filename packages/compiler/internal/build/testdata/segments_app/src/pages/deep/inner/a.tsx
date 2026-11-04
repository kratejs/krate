import { useState } from 'react';
function Bomb({ armed }) {
  if (armed) throw new Error('deep page exploded');
  return <p id="deep-a">deep a</p>;
}
export default function DeepA() {
  const [armed, setArmed] = useState(false);
  return (
    <div>
      <button id="deep-arm" onClick={() => setArmed(true)}>Arm</button>
      <Bomb armed={armed} />
    </div>
  );
}
