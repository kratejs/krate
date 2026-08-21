import './select.css';
import { createSignal, createEffect } from '@krate/runtime';

export interface SelectProps {
  children?: any;
  value?: string;
  defaultValue?: string;
  onValueChange?: (value: string) => void;
  disabled?: boolean;
  placeholder?: string;
}

export default function Select(props: SelectProps) {
  var placeholder = props.placeholder || "Select...";
  var [open, setOpen] = createSignal(false);
  var [selectedValue, setSelectedValue] = createSignal(props.defaultValue || "");
  var [selectedLabel, setSelectedLabel] = createSignal("");
  var [filter, setFilter] = createSignal("");
  var [focusedIndex, setFocusedIndex] = createSignal(0);
  var rootRef: HTMLElement | null = null;

  createEffect(function () {
    if (props.value !== undefined) {
      setSelectedValue(props.value);
    }
  });

  function syncItems(root: HTMLElement, sv: string) {
    var items = root.querySelectorAll("[data-krate-select-item]");
    for (var i = 0; i < items.length; i++) {
      var el = items[i] as HTMLElement;
      var val = el.getAttribute("data-value");
      el.setAttribute("data-selected", val === sv ? "true" : "false");
    }
  }

  createEffect(function () {
    var sv = selectedValue();
    var root = rootRef;
    if (root) {
      syncItems(root, sv);
    }
  });

  function selectOption(value: string, label: string) {
    setSelectedValue(value);
    setSelectedLabel(label);
    setOpen(false);
    setFilter("");
    applyFilter("");
    if (props.onValueChange) {
      props.onValueChange(value);
    }
  }

  function handleClick(e: MouseEvent) {
    var target = e.target as HTMLElement;
    var root = target.closest(".krate-select") as HTMLElement;
    if (root) rootRef = root;

    var trigger = target.closest("[data-krate-select-trigger]");
    if (trigger) {
      if (props.disabled) return;
      setOpen(!open());
      if (!open()) {
        setFilter("");
        applyFilter("");
        setFocusedIndex(0);
      }
      return;
    }
    var item = target.closest("[data-krate-select-item]");
    if (item && open()) {
      var val = (item as HTMLElement).getAttribute("data-value");
      var label = "";
      var textEl = (item as HTMLElement).querySelector(".krate-select-item-text");
      if (textEl) {
        label = textEl.textContent || "";
      }
      if (val) {
        selectOption(val, label);
      }
      return;
    }
  }

  function handleFilterInput(e: Event) {
    var target = e.target as HTMLInputElement;
    setFilter(target.value);
    setFocusedIndex(0);
    applyFilter(target.value);
  }

  function applyFilter(query: string) {
    var root = rootRef;
    if (!root) return;
    var q = query.trim().toLowerCase();
    var items = root.querySelectorAll("[data-krate-select-item]");
    var hasVisible = false;
    for (var i = 0; i < items.length; i++) {
      var el = items[i] as HTMLElement;
      var textEl = el.querySelector(".krate-select-item-text");
      var text = (textEl ? textEl.textContent : "") || "";
      var show = q === "" || text.toLowerCase().indexOf(q) >= 0;
      el.style.display = show ? "" : "none";
      if (show) hasVisible = true;
    }
    // Hide group headings whose items are all filtered out (e.g. typing a
    // search term that only matches one group's items should hide the other
    // groups' labels and separators).
    var groups = root.querySelectorAll(".krate-select-group");
    for (var gi = 0; gi < groups.length; gi++) {
      var group = groups[gi] as HTMLElement;
      var groupItems = group.querySelectorAll("[data-krate-select-item]");
      var groupHasVisible = false;
      for (var j = 0; j < groupItems.length; j++) {
        if ((groupItems[j] as HTMLElement).style.display !== "none") {
          groupHasVisible = true;
          break;
        }
      }
      var label = group.querySelector(".krate-select-group-label") as HTMLElement | null;
      if (label) {
        label.style.display = groupHasVisible ? "" : "none";
      }
    }
    var seps = root.querySelectorAll(".krate-select-separator");
    for (var si = 0; si < seps.length; si++) {
      var sep = seps[si] as HTMLElement;
      var sepVisible = false;
      var prev = sep.previousElementSibling as HTMLElement | null;
      var next = sep.nextElementSibling as HTMLElement | null;
      var neighbors = [prev, next];
      for (var ni = 0; ni < neighbors.length; ni++) {
        var n = neighbors[ni];
        if (!n) continue;
        var nItems = n.querySelectorAll("[data-krate-select-item]");
        for (var k = 0; k < nItems.length; k++) {
          if ((nItems[k] as HTMLElement).style.display !== "none") {
            sepVisible = true;
            break;
          }
        }
        if (sepVisible) break;
      }
      sep.style.display = sepVisible ? "" : "none";
    }
    var empty = root.querySelector(".krate-select-empty");
    if (empty) {
      (empty as HTMLElement).style.display = hasVisible || q === "" ? "none" : "";
    }
  }

  function handleFilterKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      setOpen(false);
      setFilter("");
    }
  }

  function handleClickOutside(e: MouseEvent) {
    var r = rootRef;
    if (r && !r.contains(e.target as Node)) {
      setOpen(false);
      setFilter("");
      applyFilter("");
    }
  }

  createEffect(function () {
    var outsideTimer: any = null;
    if (open()) {
      outsideTimer = setTimeout(function () {
        document.addEventListener("mousedown", handleClickOutside);
      }, 0);
    }
    return function () {
      if (outsideTimer) clearTimeout(outsideTimer);
      document.removeEventListener("mousedown", handleClickOutside);
    };
  });

  return (
    <div class={"krate-select" + (props.disabled ? " krate-select-disabled" : "")} data-state={open() ? "open" : "closed"} onClick={handleClick}>
      <button
        class="krate-select-trigger"
        type="button"
        data-krate-select-trigger="true"
        disabled={props.disabled}
        aria-haspopup="listbox"
        aria-expanded={open() ? "true" : "false"}
      >
        <span class="krate-select-value">
          {selectedValue() !== "" ? selectedLabel() || selectedValue() : placeholder}
        </span>
        <span class="krate-select-icon">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="m6 9 6 6 6-6"/>
          </svg>
        </span>
      </button>
      <div class="krate-select-content" role="listbox">
        <div class="krate-select-filter-wrapper">
          <input
            class="krate-select-filter"
            type="text"
            placeholder="Type to search..."
            value={filter()}
            onInput={handleFilterInput}
            onKeyDown={handleFilterKeyDown}
          />
        </div>
        <div class="krate-select-viewport">
          {props.children}
          <div class="krate-select-empty" style="display: none;">No results found</div>
        </div>
      </div>
    </div>
  );
}

export interface SelectItemProps {
  children?: any;
  value?: string;
  disabled?: boolean;
}

export function SelectItem(props: SelectItemProps) {
  var value = props.value || "";
  var children = props.children || "";

  return (
    <div
      class={"krate-select-item" + (props.disabled ? " krate-select-item-disabled" : "")}
      role="option"
      data-value={value}
      data-selected="false"
      data-disabled={props.disabled ? "true" : "false"}
      data-krate-select-item="true"
    >
      <span class="krate-select-item-indicator">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="20 6 9 17 4 12"/>
        </svg>
      </span>
      <span class="krate-select-item-text">{children}</span>
    </div>
  );
}

export interface SelectGroupProps {
  children?: any;
  label?: string;
}

export function SelectGroup(props: SelectGroupProps) {
  return (
    <div class="krate-select-group">
      {props.label ? <div class="krate-select-group-label">{props.label}</div> : null}
      {props.children}
    </div>
  );
}

export interface SelectSeparatorProps {
}

export function SelectSeparator(props: SelectSeparatorProps) {
  return <div class="krate-select-separator" />;
}

export interface SelectLabelProps {
  children?: any;
}

export function SelectLabel(props: SelectLabelProps) {
  return <div class="krate-select-label">{props.children}</div>;
}
