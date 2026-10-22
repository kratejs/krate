import { useEffect, useState } from "react";
import { log } from "../lib/log";

// When several effects in one component re-run, React runs every cleanup
// first and then every new effect, in declaration order.
export default function EffectCleanupOrderRerun() {
  const [room, setRoom] = useState("general");

  useEffect(() => {
    log("connect", room);
    return () => log("disconnect", room);
  }, [room]);

  useEffect(() => {
    log("subscribe presence", room);
    return () => log("unsubscribe presence", room);
  }, [room]);

  useEffect(() => {
    document.title = "Room " + room;
    log("title set", room);
    return () => log("title reset", room);
  }, [room]);

  return (
    <div>
      <h1 id="room">{room}</h1>
      <button id="random" onClick={() => setRoom("random")}>
        random
      </button>
      <button id="general" onClick={() => setRoom("general")}>
        general
      </button>
    </div>
  );
}
