import { Fragment, useState } from "react";

type Term = { id: string; term: string; def: string };

// Keyed fragments render several siblings per item without a wrapper.
export default function KeyFragment() {
  const [terms, setTerms] = useState<Term[]>([
    { id: "a", term: "HTML", def: "markup" },
    { id: "b", term: "CSS", def: "style" },
  ]);
  return (
    <div>
      <dl>
        {terms.map((t) => (
          <Fragment key={t.id}>
            <dt>{t.term}</dt>
            <dd>{t.def}</dd>
          </Fragment>
        ))}
      </dl>
      <button id="add" onClick={() => setTerms([{ id: "c", term: "JS", def: "behavior" }, ...terms])}>
        add
      </button>
      <button id="drop" onClick={() => setTerms(terms.filter((t) => t.id !== "a"))}>
        drop html
      </button>
    </div>
  );
}
