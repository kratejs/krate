import { useState } from "react";
import { log } from "../lib/log";

// onKeyDown exposes key, and handlers can act on Enter and Escape.
export default function EventKeydownKey() {
  const [tags, setTags] = useState<string[]>([]);
  const [draft, setDraft] = useState("");
  return (
    <div>
      <input
        id="tag"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          log("keydown", e.key, "shift", e.shiftKey);
          if (e.key === "Enter" && draft.trim()) {
            setTags([...tags, draft.trim()]);
            setDraft("");
          } else if (e.key === "Escape") {
            setDraft("");
          }
        }}
      />
      <ul>
        {tags.map((t) => (
          <li key={t}>{t}</li>
        ))}
      </ul>
    </div>
  );
}
