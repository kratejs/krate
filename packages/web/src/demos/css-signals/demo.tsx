import { createCSSChoice, createCSSGroup, createCSSRange } from "@krate/runtime";
import "./demo.css";

// Every widget here keeps its state in hidden inputs and :has() CSS, so the
// page ships no JavaScript for it. Turn JavaScript off and they still work.
export default function Demo() {
  const billing = createCSSChoice("monthly", {
    options: ["monthly", "yearly"],
    label: "Billing period",
    vars: {
      "--price": { monthly: '"$12"', yearly: '"$9"' },
      "--period": { monthly: '"month to month"', yearly: '"once a year"' },
    },
  });
  const topic = createCSSChoice("all", ["all", "signals", "css", "go"]);
  const faq = createCSSGroup(null);
  const level = createCSSRange(1, { min: 0, max: 3, step: 1 });

  return (
    <div class="cz">
      <section class="cz-block">
        <h2>Choice with live text</h2>
        <div class="cz-seg">
          <button type="button" {...billing.trigger("monthly")}>Monthly</button>
          <button type="button" {...billing.trigger("yearly")}>Yearly</button>
        </div>
        <p class="cz-price">
          <span class="cz-amount"></span> per seat / month, <span class="cz-period"></span>
        </p>
      </section>

      <section class="cz-block">
        <h2>Choice swaps panels</h2>
        <div class="cz-chips">
          <button type="button" {...topic.trigger("all")}>All</button>
          <button type="button" {...topic.trigger("signals")}>Signals</button>
          <button type="button" {...topic.trigger("css")}>CSS</button>
          <button type="button" {...topic.trigger("go")}>Go</button>
        </div>
        <div {...topic.panel("all")}>
          <ul class="cz-list">
            <li>createSignal and createMemo</li>
            <li>CSS Modules and Tailwind</li>
            <li>Go components and API routes</li>
          </ul>
        </div>
        <div {...topic.panel("signals")}><ul class="cz-list"><li>createSignal and createMemo</li></ul></div>
        <div {...topic.panel("css")}><ul class="cz-list"><li>CSS Modules and Tailwind</li></ul></div>
        <div {...topic.panel("go")}><ul class="cz-list"><li>Go components and API routes</li></ul></div>
      </section>

      <section class="cz-block">
        <h2>Accordion</h2>
        <button type="button" class="cz-q" {...faq.trigger("one")}>Where does the state live?</button>
        <div class="cz-a" {...faq.panel("one")}>In the DOM: a hidden radio input that :has() reads.</div>
        <button type="button" class="cz-q" {...faq.trigger("two")}>Can it be closed again?</button>
        <div class="cz-a" {...faq.panel("two")}>Yes. Clicking an open question closes it.</div>
      </section>

      <section class="cz-block">
        <h2>Range</h2>
        <div class="cz-bar"><div class="cz-fill krc-fill"></div></div>
        <p class="cz-step">Level {level.current()} of 3</p>
        <div class="cz-chips">
          <button type="button" {...level.trigger(0)}>0</button>
          <button type="button" {...level.trigger(1)}>1</button>
          <button type="button" {...level.trigger(2)}>2</button>
          <button type="button" {...level.trigger(3)}>3</button>
        </div>
      </section>
    </div>
  );
}
