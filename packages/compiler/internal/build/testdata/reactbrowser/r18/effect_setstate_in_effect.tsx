import { useEffect, useState } from "react";
import { log } from "../lib/log";

// An effect that derives state triggers one more render; the derived value
// is visible after the update settles.
export default function EffectSetStateInEffect() {
  const [text, setText] = useState("hello");
  const [words, setWords] = useState(0);

  useEffect(() => {
    const n = text.trim() === "" ? 0 : text.trim().split(/\s+/).length;
    log("count words", text, n);
    setWords(n);
  }, [text]);

  useEffect(() => {
    log("words now", words);
  }, [words]);

  return (
    <div>
      <input id="text" value={text} onChange={(e) => setText(e.target.value)} />
      <p id="words">{words} words</p>
    </div>
  );
}
