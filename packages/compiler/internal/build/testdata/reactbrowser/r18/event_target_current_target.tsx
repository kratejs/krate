import { log } from "../lib/log";

// event.target is the clicked element; event.currentTarget is the element
// whose handler is running.
export default function EventTargetCurrentTarget() {
  const describe = (el: EventTarget | null) => {
    const e = el as HTMLElement | null;
    return e ? e.tagName.toLowerCase() + (e.id ? "#" + e.id : "") : "null";
  };
  return (
    <nav
      id="nav"
      onClick={(e) => log("nav handler target", describe(e.target), "current", describe(e.currentTarget))}
    >
      <a
        id="link"
        href="#section"
        onClick={(e) => {
          e.preventDefault();
          log("link handler target", describe(e.target), "current", describe(e.currentTarget));
        }}
      >
        <strong id="bold">Docs</strong> section
      </a>
    </nav>
  );
}
