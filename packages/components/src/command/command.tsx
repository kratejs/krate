import './command.css';
import { createSignal, onMount } from '@krate/runtime';

export interface CommandItem {
  value: string;
  label: string;
  keywords?: string;
  group?: string;
}

export interface CommandProps {
  items: CommandItem[];
  placeholder?: string;
  emptyText?: string;
  /** Accessible name for the input and the results list. */
  label?: string;
  class?: string;
  /** Base for the ids that link the input to its list (chosen automatically when omitted). */
  id?: string;
  onSelect?: (value: string) => void;
}

// Command is a searchable list (command palette / combobox list). Filtering is
// client-side over the supplied items; selection is reported via onSelect.
export function Command(props: CommandProps) {
  // Unique per instance, so it is chosen on mount unless the author passes `id`.
  var [listIdSig, setListId] = createSignal(props.id || "");
  var [query, setQuery] = createSignal('');
  var [active, setActive] = createSignal(0);
  onMount(function () {
    if (!listIdSig()) setListId("krate-command-list-" + Math.random().toString(36).slice(2, 8));
  });
  var className = "krate-command";
  if (props.class) className += " " + props.class;
  var items = props.items || [];

  // Pure on purpose: the markup calls it as matchItems(items, query()) so the
  // compiler sees the query signal being read and re-renders the list with it.
  function matchItems(list: CommandItem[], q: string): CommandItem[] {
    q = q.toLowerCase();
    if (q === "") return list;
    var out: CommandItem[] = [];
    for (var i = 0; i < list.length; i++) {
      var it = list[i];
      var hay = (it.label + " " + (it.keywords || "") + " " + it.value).toLowerCase();
      if (hay.indexOf(q) >= 0) out.push(it);
    }
    return out;
  }

  function visible(): CommandItem[] {
    return matchItems(items, query());
  }

  function select(value: string) {
    if (props.onSelect) props.onSelect(value);
  }

  function handleInput(e: Event) {
    var t = e.target as HTMLInputElement;
    setQuery(t.value);
    setActive(0);
  }

  // Keeps the active option in view when the list scrolls.
  function reveal(e: Event) {
    var root = (e.target as HTMLElement).closest("[data-krate-command]");
    requestAnimationFrame(function () {
      var el = root ? (root.querySelector("[aria-selected='true']") as HTMLElement | null) : null;
      if (el && el.scrollIntoView) el.scrollIntoView({ block: "nearest" });
    });
  }

  function handleKeyDown(e: KeyboardEvent) {
    // Keys pressed while an IME composes text belong to the IME.
    if (e.isComposing) return;
    var list = visible();
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive(Math.min(active() + 1, list.length - 1));
      reveal(e);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(Math.max(active() - 1, 0));
      reveal(e);
    } else if (e.key === "Home") {
      e.preventDefault();
      setActive(0);
      reveal(e);
    } else if (e.key === "End") {
      e.preventDefault();
      setActive(Math.max(list.length - 1, 0));
      reveal(e);
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (list[active()]) select(list[active()].value);
    }
  }

  function handleClick(e: MouseEvent) {
    var target = e.target as HTMLElement;
    var item = target.closest("[data-command-value]") as HTMLElement;
    if (item) {
      var value = item.getAttribute("data-command-value");
      if (value) select(value);
    }
  }

  return (
    <div class={className} data-krate-command="true" onClick={handleClick}>
      <input
        class="krate-command-input"
        type="text"
        role="combobox"
        aria-expanded={matchItems(items, query()).length > 0 ? "true" : "false"}
        aria-autocomplete="list"
        aria-controls={listIdSig()}
        aria-activedescendant={matchItems(items, query()).length > 0 ? listIdSig() + "-opt-" + active() : undefined}
        aria-label={props.label || props.placeholder || "Search"}
        placeholder={props.placeholder || "Type to search..."}
        onInput={handleInput}
        onKeyDown={handleKeyDown}
      />
      <ul class="krate-command-list" role="listbox" id={listIdSig()} aria-label={props.label || "Results"}>
        {matchItems(items, query()).map((item, i) => (
          <li
            key={item.value}
            id={listIdSig() + "-opt-" + i}
            class={"krate-command-item" + (i === active() ? " krate-command-item-active" : "")}
            role="option"
            aria-selected={i === active() ? "true" : "false"}
            data-command-value={item.value}
          >
            <span class="krate-command-label">{item.label}</span>
            {item.group ? <span class="krate-command-group">{item.group}</span> : null}
          </li>
        ))}
        {matchItems(items, query()).length === 0 ? (
          <li class="krate-command-empty">{props.emptyText || "No results."}</li>
        ) : null}
      </ul>
    </div>
  );
}
