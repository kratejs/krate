import { useEffect, useState } from "react";
import { log } from "../lib/log";

function wait(ms: number, v: string): Promise<string> {
  return new Promise((r) => setTimeout(() => r(v), ms));
}

function Loader(props: { q: string }) {
  const [res, setRes] = useState("none");
  useEffect(() => {
    let live = true;
    log("start", props.q);
    wait(props.q === "slow" ? 60 : 10, props.q).then((v) => {
      if (live) {
        log("apply", v);
        setRes(v);
      } else {
        log("ignored", v);
      }
    });
    return () => {
      live = false;
      log("cancel", props.q);
    };
  }, [props.q]);
  return <p id="res">{res}</p>;
}

export default function EffectAsyncRaceCancel() {
  const [q, setQ] = useState("slow");
  const [show, setShow] = useState(true);
  return (
    <div>
      <button id="fast" onClick={() => setQ("fast")}>fast</button>
      <button id="slow" onClick={() => setQ("slow")}>slow</button>
      <button id="hide" onClick={() => setShow(false)}>hide</button>
      {show && <Loader q={q} />}
    </div>
  );
}
