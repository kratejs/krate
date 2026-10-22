import { useState } from "react";

const tabs = [
  { id: "overview", label: "Overview", body: "Krate compiles TSX." },
  { id: "pricing", label: "Pricing", body: "Free and open source." },
  { id: "faq", label: "FAQ", body: "" },
];

const faqs = [
  { q: "Is it fast?", a: "Yes." },
  { q: "Does it hydrate?", a: "With signals." },
  { q: "React?", a: "Source components." },
];

function Accordion(props: { multiple: boolean }) {
  const [open, setOpen] = useState<number[]>([0]);
  return (
    <div className="accordion">
      {faqs.map((f, i) => {
        const isOpen = open.includes(i);
        return (
          <div key={f.q} className="item">
            <button
              id={"q" + i}
              aria-expanded={isOpen}
              onClick={() =>
                setOpen(isOpen ? open.filter((x) => x !== i) : props.multiple ? [...open, i] : [i])
              }
            >
              {f.q}
            </button>
            {isOpen && <p className="answer">{f.a}</p>}
          </div>
        );
      })}
    </div>
  );
}

// Tabs with keyboard navigation and an accordion inside one tab that keeps
// its state while hidden tabs are unmounted.
export default function AppTabsAccordion() {
  const [active, setActive] = useState("overview");
  const [multiple, setMultiple] = useState(false);
  const index = tabs.findIndex((t) => t.id === active);
  return (
    <div>
      <div role="tablist" onKeyDown={(e) => {
        if (e.key === "ArrowRight") setActive(tabs[(index + 1) % tabs.length].id);
        if (e.key === "ArrowLeft") setActive(tabs[(index + tabs.length - 1) % tabs.length].id);
      }}>
        {tabs.map((t) => (
          <button
            key={t.id}
            id={"tab-" + t.id}
            role="tab"
            aria-selected={active === t.id}
            tabIndex={active === t.id ? 0 : -1}
            onClick={() => setActive(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id="panel">
        {active === "faq" ? (
          <>
            <label>
              <input id="multiple" type="checkbox" checked={multiple} onChange={(e) => setMultiple(e.target.checked)} />
              allow multiple
            </label>
            <Accordion multiple={multiple} />
          </>
        ) : (
          <p>{tabs[index].body}</p>
        )}
      </div>
    </div>
  );
}
