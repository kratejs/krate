import { createContext, createSignal, createMemo } from "@krate/runtime";
import "./demo.css";

type Line = { id: string; qty: number };
const CATALOG = [
  { id: "keyboard", name: "Keyboard", price: 89 },
  { id: "mouse", name: "Mouse", price: 49 },
  { id: "monitor", name: "Monitor", price: 249 },
];

// The cart lives in module scope, so any component can import and read it
// without props. persist keeps it across reloads and tabs.
const [lines, setLines] = createSignal<Line[]>([], { persist: "krate-demo-cart" });
const count = createMemo(() => lines().reduce((n, l) => n + l.qty, 0));
const total = createMemo(() =>
  lines().reduce((sum, l) => sum + l.qty * CATALOG.find((p) => p.id === l.id)!.price, 0),
);
const add = (id: string) => {
  const has = lines().some((l) => l.id === id);
  setLines(has ? lines().map((l) => (l.id === id ? { ...l, qty: l.qty + 1 } : l)) : [...lines(), { id, qty: 1 }]);
};
const remove = (id: string) => setLines(lines().filter((l) => l.id !== id));

// A context carries the currency symbol down to every price.
const SymbolCtx = createContext("$");

function Shop() {
  const symbol = SymbolCtx.useContext();
  return (
    <ul class="cx-list">
      {CATALOG.map((p) => (
        <li>
          <span>{p.name} <span class="dk-muted">{symbol}{p.price}</span></span>
          <button type="button" class="dk-btn" aria-label={"Add " + p.name + " to the cart"} onClick={() => add(p.id)}>Add</button>
        </li>
      ))}
    </ul>
  );
}

function CartBadge() {
  return <p class="cx-badge" aria-live="polite">Cart: <strong>{count()}</strong> items</p>;
}

function CartPanel() {
  const symbol = SymbolCtx.useContext();
  return (
    <div>
      <ul class="cx-list">
        {lines().map((l) => (
          <li key={l.id}>
            <span>{l.qty} x {l.id}</span>
            <button type="button" class="cx-x" aria-label={"Remove " + l.id} onClick={() => remove(l.id)}>&times;</button>
          </li>
        ))}
      </ul>
      <p class="cx-empty dk-muted" showIf={count() === 0}>The cart is empty.</p>
      <p class="cx-total">Total <strong>{symbol}{total()}</strong></p>
      <button type="button" class="dk-btn" onClick={() => setLines([])}>Clear</button>
    </div>
  );
}

export default function Demo() {
  const [symbol, setSymbol] = createSignal("$");
  return (
    <SymbolCtx.Provider value={symbol()}>
      <div class="dk-row cx-bar">
        <button type="button" class="dk-btn" aria-pressed={symbol() === "EUR "} onClick={() => setSymbol(symbol() === "$" ? "EUR " : "$")}>
          Show prices in EUR
        </button>
        <span class="dk-muted">Symbol: {symbol()}</span>
      </div>
      <div class="cx">
        <section aria-label="Shop"><h2>Shop</h2><Shop /></section>
        <section aria-label="Cart"><h2>Cart</h2><CartBadge /><CartPanel /></section>
      </div>
    </SymbolCtx.Provider>
  );
}
