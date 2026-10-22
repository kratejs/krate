import { log } from "../lib/log";

// Click events bubble from the target through every ancestor handler.
export default function EventBubbling() {
  return (
    <div id="outer" onClick={() => log("outer click")}>
      <ul id="list" onClick={(e) => log("list click", (e.target as HTMLElement).id)}>
        <li id="item" onClick={() => log("item click")}>
          <button id="btn" onClick={() => log("button click")}>
            open
          </button>
          <span id="text">plain text</span>
        </li>
      </ul>
    </div>
  );
}
