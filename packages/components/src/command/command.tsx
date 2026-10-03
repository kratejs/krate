import './command.css';
import { createSignal } from '@krate/runtime';

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
  class?: string;
  onSelect?: (value: string) => void;
}

// Command is a searchable list (command palette / combobox list). Filtering is
// client-side over the supplied items; selection is reported via onSelect.
export function Command(props: CommandProps) {
  var [query, setQuery] = createSignal('');
  var [active, setActive] = createSignal(0);
  var className = "krate-command";
  if (props.class) className += " " + props.class;
  var items = props.items || [];

  function visible(): CommandItem[] {
    var q = query().toLowerCase();
    if (q === "") return items;
    var out: CommandItem[] = [];
    for (var i = 0; i < items.length; i++) {
      var it = items[i];
      var hay = (it.label + " " + (it.keywords || "") + " " + it.value).toLowerCase();
      if (hay.indexOf(q) >= 0) out.push(it);
    }
    return out;
  }

  function select(value: string) {
    if (props.onSelect) props.onSelect(value);
  }

  function handleInput(e: Event) {
    var t = e.target as HTMLInputElement;
    setQuery(t.value);
    setActive(0);
  }

  function handleKeyDown(e: KeyboardEvent) {
    var list = visible();
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive(Math.min(active() + 1, list.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(Math.max(active() - 1, 0));
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
        aria-expanded="true"
        aria-autocomplete="list"
        placeholder={props.placeholder || "Type to search..."}
        onInput={handleInput}
        onKeyDown={handleKeyDown}
      />
      <ul class="krate-command-list" role="listbox">
        {visible().map((item, i) => (
          <li
            class={"krate-command-item" + (i === active() ? " krate-command-item-active" : "")}
            role="option"
            aria-selected={i === active() ? "true" : "false"}
            data-command-value={item.value}
          >
            <span class="krate-command-label">{item.label}</span>
            {item.group ? <span class="krate-command-group">{item.group}</span> : null}
          </li>
        ))}
        {visible().length === 0 ? (
          <li class="krate-command-empty">{props.emptyText || "No results."}</li>
        ) : null}
      </ul>
    </div>
  );
}
