import { createContext, useContext, useState } from "react";

const Currency = createContext({ code: "USD", symbol: "$" });

function Price(props: { amount: number }) {
  const c = useContext(Currency);
  return (
    <span className="price">
      {c.symbol}
      {props.amount.toFixed(2)} {c.code}
    </span>
  );
}

// Without a provider, consumers read the context's default value; a provider
// can be added later.
export default function ContextDefaultValue() {
  const [euro, setEuro] = useState(false);
  return (
    <div>
      <p id="default">
        <Price amount={9.5} />
      </p>
      {euro ? (
        <Currency.Provider value={{ code: "EUR", symbol: "€" }}>
          <p id="provided">
            <Price amount={9.5} />
          </p>
        </Currency.Provider>
      ) : (
        <p id="provided">
          <Price amount={9.5} />
        </p>
      )}
      <button id="euro" onClick={() => setEuro(true)}>
        euro
      </button>
    </div>
  );
}
