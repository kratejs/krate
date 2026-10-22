import { log } from "../lib/log";

// Synthetic event fields mirror the native event: type, modifier keys,
// button, bubbles, and access to nativeEvent.
export default function EventSyntheticFields() {
  return (
    <div>
      <button
        id="btn"
        onClick={(e) => {
          log(
            "click",
            e.type,
            "shift",
            e.shiftKey,
            "alt",
            e.altKey,
            "button",
            e.button,
            "bubbles",
            e.bubbles,
            "native",
            e.nativeEvent instanceof MouseEvent,
          );
        }}
        onContextMenu={(e) => {
          e.preventDefault();
          log("contextmenu", e.button, e.defaultPrevented);
        }}
      >
        inspect
      </button>
    </div>
  );
}
