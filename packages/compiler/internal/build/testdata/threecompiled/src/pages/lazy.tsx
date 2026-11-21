// A dynamic import() makes the build keep the whole hydration runtime, which
// @krate/three reads through globals the page code does not name.
export default function Lazy() {
  return <button onClick={() => import("../lazy-data")}>load</button>;
}
