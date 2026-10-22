import { log } from "../lib/log";

// Capture handlers run top-down before bubble handlers run bottom-up.
export default function EventCapture() {
  return (
    <section
      id="section"
      onClickCapture={() => log("section capture")}
      onClick={() => log("section bubble")}
    >
      <div onClickCapture={() => log("div capture")} onClick={() => log("div bubble")}>
        <button id="btn" onClickCapture={() => log("button capture")} onClick={() => log("button bubble")}>
          press
        </button>
      </div>
    </section>
  );
}
