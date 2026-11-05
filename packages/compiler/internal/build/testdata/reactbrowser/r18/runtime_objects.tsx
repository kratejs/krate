import { createContext, useContext, useMemo, useReducer, useRef, useState } from "react";

const TAGS = new Set(["a", "b"]);
const LOOKUP = new Map([["k", "module-map"]]);
const WHEN = new Date("2020-01-02T03:04:05Z");
const NESTED = { seen: new Set([1]), m: new Map([["x", 2]]) };
const Ctx = createContext(new Set(["ctx"]));

function Child({ set, map, when }: { set: Set<string>; map: Map<string, string>; when: Date }) {
  const [n, setN] = useState(0);
  return (
    <p id="child" onClick={() => setN(n + 1)}>
      {set.has("p") ? "yes" : "no"} {map.get("q")} {when.getUTCFullYear()} {n}
    </p>
  );
}

function Reader() {
  const c = useContext(Ctx);
  const [n, setN] = useState(0);
  return (
    <p id="ctx" onClick={() => setN(n + 1)}>
      {c.has("ctx") ? "ctx-ok" : "ctx-no"} {n}
    </p>
  );
}

// Map, Set and Date in state, refs, reducers, context, module constants and
// props: the build evaluates them, but they have no literal form for the client.
export default function RuntimeObjects() {
  const [s, setS] = useState(new Set([1]));
  const [m, setM] = useState(() => new Map([["a", 1]]));
  const [o, setO] = useState({ tags: new Set(["t"]) });
  const [d] = useState(new Date("2021-06-07T00:00:00Z"));
  const ref = useRef(new Map([["r", "ref-ok"]]));
  const [r, dispatch] = useReducer((st: Set<number>) => new Set([...st, st.size + 1]), new Set([1]));
  const local = new Set(["l"]);
  const memo = useMemo(() => new Map([["m", s.size]]), [s]);
  return (
    <main>
      <button
        id="go"
        onClick={() => {
          setS(new Set([...s, s.size + 1]));
          setM(new Map([...m, ["b", 2]]));
          setO({ tags: new Set([...o.tags, "u"]) });
          dispatch();
        }}
      >
        go
      </button>
      <p id="a">{s.has(1) ? "s-ok" : "s-no"} {s.size}</p>
      <p id="b">{m.get("a")} {m.size}</p>
      <p id="c">{o.tags.has("t") ? "o-ok" : "o-no"} {o.tags.size}</p>
      <p id="d">{d.getUTCFullYear()}</p>
      <p id="e">{ref.current.get("r")}</p>
      <p id="f">{r.size}</p>
      <p id="g">{local.has("l") ? "l-ok" : "l-no"} {memo.get("m")}</p>
      <p id="h">
        {TAGS.has("a") ? "t-ok" : "t-no"} {LOOKUP.get("k")} {WHEN.getUTCFullYear()} {NESTED.seen.has(1) ? "n-ok" : "n-no"} {NESTED.m.get("x")}
      </p>
      <Child set={new Set(["p"])} map={new Map([["q", "child-map"]])} when={new Date("2019-01-01T00:00:00Z")} />
      <Reader />
    </main>
  );
}
