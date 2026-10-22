import { useState } from "react";

function Task(props: { title: string }) {
  const [done, setDone] = useState(false);
  return (
    <li>
      <label>
        <input type="checkbox" className="done" checked={done} onChange={() => setDone(!done)} />
        {props.title}
      </label>
    </li>
  );
}

// Index keys attach state to positions, not items: after removing the first
// item, the checked state stays at index 0 (React's documented behavior).
export default function KeyIndexVsId() {
  const [tasks, setTasks] = useState(["wash", "dry", "fold"]);
  return (
    <div>
      <ul id="by-index">
        {tasks.map((t, i) => (
          <Task key={i} title={t} />
        ))}
      </ul>
      <ul id="by-id">
        {tasks.map((t) => (
          <Task key={t} title={t} />
        ))}
      </ul>
      <button id="shift" onClick={() => setTasks(tasks.slice(1))}>
        remove first
      </button>
    </div>
  );
}
