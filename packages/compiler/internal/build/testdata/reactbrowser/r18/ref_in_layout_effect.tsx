import { useLayoutEffect, useRef, useState } from "react";
import { log } from "../lib/log";

// A layout effect measures a ref'd element after the DOM update and before
// paint, then positions a tooltip from the measurement.
export default function RefInLayoutEffect() {
  const box = useRef<HTMLDivElement>(null);
  const [lines, setLines] = useState(1);
  const [height, setHeight] = useState(0);

  useLayoutEffect(() => {
    const h = box.current!.getBoundingClientRect().height;
    log("measured lines", lines, "taller than before", h > height);
    setHeight(h);
  }, [lines]);

  return (
    <div>
      <div id="box" ref={box} style={{ lineHeight: "20px", width: 200 }}>
        {Array.from({ length: lines }, (_, i) => (
          <div key={i}>line {i + 1}</div>
        ))}
      </div>
      <p id="height">{Math.round(height)}px</p>
      <button id="more" onClick={() => setLines(lines + 1)}>
        more
      </button>
    </div>
  );
}
