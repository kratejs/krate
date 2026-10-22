import { useState } from "react";

// Duplicate keys are a bug React only warns about in development; in
// production both items still render. Removing one keeps the list sane.
export default function KeyDuplicate() {
  const [tags, setTags] = useState(["red", "blue", "red"]);
  return (
    <div>
      <ul>
        {tags.map((t, i) => (
          <li key={t}>
            {t} #{i}
          </li>
        ))}
      </ul>
      <button id="add" onClick={() => setTags([...tags, "green"])}>
        add green
      </button>
      <button id="pop" onClick={() => setTags(tags.slice(0, -1))}>
        pop
      </button>
    </div>
  );
}
