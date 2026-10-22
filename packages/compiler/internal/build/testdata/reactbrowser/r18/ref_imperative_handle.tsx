import { forwardRef, useImperativeHandle, useRef, useState } from "react";
import { log } from "../lib/log";

type PlayerHandle = { play: () => void; describe: () => string };

const Player = forwardRef<PlayerHandle, { track: string }>(function Player(props, ref) {
  const [playing, setPlaying] = useState(false);
  useImperativeHandle(
    ref,
    () => {
      log("create handle", props.track);
      return {
        play: () => setPlaying(true),
        describe: () => props.track + (playing ? " (playing)" : ""),
      };
    },
    [props.track, playing],
  );
  return (
    <p id="player">
      {props.track} {playing ? "playing" : "stopped"}
    </p>
  );
});

// useImperativeHandle exposes a custom handle; it is recreated only when its
// dependencies change.
export default function RefImperativeHandle() {
  const player = useRef<PlayerHandle>(null);
  const [track, setTrack] = useState("intro");
  const [n, setN] = useState(0);
  return (
    <div>
      <Player ref={player} track={track} />
      <button id="play" onClick={() => player.current!.play()}>
        play
      </button>
      <button id="describe" onClick={() => log("describe", player.current!.describe())}>
        describe
      </button>
      <button id="next" onClick={() => setTrack("outro")}>
        next track
      </button>
      <button id="rerender" onClick={() => setN(n + 1)}>
        rerender {n}
      </button>
    </div>
  );
}
