import { memo, useDeferredValue, useState } from "react";

const Results = memo(function Results(props: { query: string }) {
  const words = ["apple", "apricot", "banana", "blueberry", "cherry"];
  const hits = words.filter((w) => w.startsWith(props.query));
  return (
    <ul id="results">
      {hits.map((h) => (
        <li key={h}>{h}</li>
      ))}
    </ul>
  );
});

// useDeferredValue(value, initialValue): the first render uses the initial
// value, then the deferred value catches up; typing updates the input first.
export default function DeferredInitialValue() {
  const [query, setQuery] = useState("a");
  const deferred = useDeferredValue(query, "");
  return (
    <div>
      <input id="q" value={query} onChange={(e) => setQuery(e.target.value)} />
      <p id="stale">{query !== deferred ? "stale" : "fresh"}</p>
      <Results query={deferred} />
    </div>
  );
}
