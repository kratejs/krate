import { Component, useState } from "react";
import { log } from "../lib/log";

class Price extends Component<{ amount: number; currency: string }> {
  shouldComponentUpdate(next: { amount: number; currency: string }) {
    const update = Math.round(next.amount) !== Math.round(this.props.amount) || next.currency !== this.props.currency;
    log("sCU", this.props.amount, "->", next.amount, update);
    return update;
  }

  componentDidUpdate() {
    log("price didUpdate", this.props.amount);
  }

  render() {
    return (
      <span id="price">
        {Math.round(this.props.amount)} {this.props.currency}
      </span>
    );
  }
}

// shouldComponentUpdate returning false skips render and componentDidUpdate;
// the props are still updated for the next comparison.
export default function ClassShouldComponentUpdate() {
  const [amount, setAmount] = useState(10);
  const [currency, setCurrency] = useState("EUR");
  return (
    <div>
      <Price amount={amount} currency={currency} />
      <button id="small" onClick={() => setAmount(amount + 0.2)}>
        +0.2
      </button>
      <button id="big" onClick={() => setAmount(amount + 1)}>
        +1
      </button>
      <button id="usd" onClick={() => setCurrency("USD")}>
        USD
      </button>
    </div>
  );
}
