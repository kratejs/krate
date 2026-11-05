import './select.css';
import { createSignal, onMount, onCleanup } from '@krate/runtime';

export interface SelectOption {
  value: string;
  label?: string;
  disabled?: boolean;
}

// Native select -------------------------------------------------------------

export interface NativeSelectProps {
  name?: string;
  label?: string;
  placeholder?: string;
  options?: SelectOption[];
  defaultValue?: string;
  disabled?: boolean;
}

/**
 * Native `<select>`. Zero JS, full keyboard/SR support, submits with a form,
 * and works with data-driven options of any size.
 */
export function NativeSelect(props: NativeSelectProps) {
  return (
    <select
      class="krate-native-select"
      name={props.name}
      aria-label={props.label}
      multiple={false}
      value={props.defaultValue}
      disabled={props.disabled}
    >
      {props.placeholder ? (
        <option value="" disabled hidden selected={!props.defaultValue}>
          {props.placeholder}
        </option>
      ) : null}
      {(props.options || []).map(function (o) {
        return (
          <option value={o.value} disabled={o.disabled}>
            {o.label || o.value}
          </option>
        );
      })}
    </select>
  );
}

// Searchable select ---------------------------------------------------------

export interface SelectProps {
  options?: SelectOption[];
  placeholder?: string;
  name?: string;
  label?: string;
  defaultValue?: string;
  disabled?: boolean;
  /** Base for the ids that link the button to its list (chosen automatically when omitted). */
  id?: string;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/**
 * Searchable select (combobox). Runs on the Krate runtime signals; use
 * `NativeSelect` when you want zero JavaScript.
 *
 * Keyboard: Enter, Space or the arrow keys on the button open the list and
 * move focus to the search field. There, ArrowUp/ArrowDown move the active
 * option, Home/End jump to the first/last, Enter chooses, Escape closes and
 * returns to the button, and Tab closes. Typing filters the list.
 */
export function Select(props: SelectProps) {
  // Unique per instance, so it is chosen on mount (not known at build time)
  // unless the author passes an `id`.
  const [uidSig, setUid] = createSignal(props.id || '');
  const options = props.options || [];
  function labelOf(v: string): string {
    for (var i = 0; i < options.length; i++) {
      if (options[i].value === v) return options[i].label || options[i].value;
    }
    return '';
  }
  const [open, setOpen] = createSignal(false);
  const [query, setQuery] = createSignal('');
  const [value, setValue] = createSignal(props.defaultValue || '');
  const [text, setText] = createSignal(labelOf(props.defaultValue || ''));
  const [active, setActive] = createSignal(-1);
  let root: HTMLElement | null = null;

  function matches(o: SelectOption, q: string): boolean {
    if (!q) return true;
    q = q.toLowerCase();
    return (o.label || o.value).toLowerCase().indexOf(q) >= 0 || o.value.toLowerCase().indexOf(q) >= 0;
  }

  // The options that match the search. Plain code can call this; the markup
  // below filters inline so the compiler sees query() as the list's input.
  function filtered(): SelectOption[] {
    const q = query();
    return options.filter(function (o) { return matches(o, q); });
  }

  // Next enabled option from `from` in direction `dir` (+1/-1), wrapping.
  function step(from: number, dir: number): number {
    const list = filtered();
    if (list.length === 0) return -1;
    var i = from;
    for (var n = 0; n < list.length; n++) {
      i = (i + dir + list.length) % list.length;
      if (!list[i].disabled) return i;
    }
    return -1;
  }

  function reveal() {
    requestAnimationFrame(function () {
      if (!root) return;
      var el = root.querySelector('[data-active="true"]') as HTMLElement | null;
      if (el && el.scrollIntoView) el.scrollIntoView({ block: 'nearest' });
    });
  }

  function focusFilter() {
    requestAnimationFrame(function () {
      if (!root) return;
      var input = root.querySelector('.krate-select-filter') as HTMLElement | null;
      if (input) input.focus();
    });
  }

  function focusTrigger() {
    if (!root) return;
    var btn = root.querySelector('.krate-select-trigger') as HTMLElement | null;
    if (btn) btn.focus();
  }

  function openList() {
    if (props.disabled) return;
    setQuery('');
    var list = options;
    var selected = -1;
    for (var i = 0; i < list.length; i++) {
      if (list[i].value === value() && !list[i].disabled) selected = i;
    }
    setActive(selected >= 0 ? selected : step(-1, 1));
    setOpen(true);
    focusFilter();
    reveal();
  }

  function closeList(refocus: boolean) {
    setOpen(false);
    setQuery('');
    if (refocus) focusTrigger();
  }

  function choose(o: SelectOption) {
    if (o.disabled) return;
    setValue(o.value);
    setText(o.label || o.value);
    closeList(true);
  }

  onMount(function () {
    function onDoc(e: MouseEvent) {
      if (root && !root.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    if (!uidSig()) setUid('krate-select-' + Math.random().toString(36).slice(2, 8));
    document.addEventListener('mousedown', onDoc);
    onCleanup(function () {
      document.removeEventListener('mousedown', onDoc);
    });
  });

  function onTriggerKeyDown(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!open()) openList();
    }
  }

  function onFilterKeyDown(e: KeyboardEvent) {
    const list = filtered();
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActive(step(active(), 1));
      reveal();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive(step(active(), -1));
      reveal();
    } else if (e.key === 'Home') {
      e.preventDefault();
      setActive(step(-1, 1));
      reveal();
    } else if (e.key === 'End') {
      e.preventDefault();
      setActive(step(0, -1));
      reveal();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (e.isComposing) return;
      var o = list[active()];
      if (o) choose(o);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      closeList(true);
    } else if (e.key === 'Tab') {
      closeList(false);
    }
  }

  // onBlur is focusout in Krate, so it also sees focus leaving a descendant.
  function onFocusOut(e: FocusEvent) {
    var next = e.relatedTarget as Node | null;
    if (next && root && !root.contains(next)) setOpen(false);
  }

  return (
    <div class={"krate-select" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} ref={function (el: Element) { root = el as HTMLElement; }} onBlur={onFocusOut}>
      <input type="hidden" name={props.name} value={value()} />
      <button
        class="krate-select-trigger"
        type="button"
        role="combobox"
        aria-haspopup="listbox"
        aria-expanded={open() ? 'true' : 'false'}
        aria-controls={open() ? uidSig() + '-list' : undefined}
        aria-label={props.label}
        disabled={props.disabled}
        onClick={function () { if (open()) { closeList(false); } else { openList(); } }}
        onKeyDown={onTriggerKeyDown}
      >
        <span class="krate-select-value">{text() || props.placeholder || 'Select...'}</span>
        <span class="krate-select-icon" aria-hidden="true">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="m6 9 6 6 6-6" />
          </svg>
        </span>
      </button>
      {open() ? (
        <div class="krate-select-content">
          <div class="krate-select-filter-wrapper">
            <input
              class="krate-select-filter"
              type="text"
              role="combobox"
              aria-expanded="true"
              aria-controls={uidSig() + '-list'}
              aria-activedescendant={active() >= 0 ? uidSig() + '-opt-' + active() : undefined}
              aria-autocomplete="list"
              placeholder="Search..."
              aria-label="Search options"
              value={query()}
              onInput={function (e: any) {
                setQuery(e.target.value);
                setActive(step(-1, 1));
              }}
              onKeyDown={onFilterKeyDown}
            />
          </div>
          <div class="krate-select-viewport" role="listbox" id={uidSig() + '-list'} aria-label={props.label}>
            {options.filter(function (o) { return matches(o, query()); }).map(function (o, i) {
              return (
                <div
                  key={o.value}
                  id={uidSig() + '-opt-' + i}
                  class={'krate-select-item' + (o.value === value() ? ' krate-select-item-selected' : '') + (o.disabled ? ' krate-select-item-disabled' : '') + (i === active() ? ' krate-select-item-active' : '')}
                  role="option"
                  data-active={i === active() ? 'true' : undefined}
                  aria-selected={o.value === value() ? 'true' : 'false'}
                  aria-disabled={o.disabled ? 'true' : undefined}
                  onMouseDown={function (e: Event) { e.preventDefault(); }}
                  onMouseMove={function () { if (!o.disabled && active() !== i) setActive(i); }}
                  onClick={function () { choose(o); }}
                >
                  <span class="krate-select-item-text">{o.label || o.value}</span>
                  {o.value === value() ? (
                    <span class="krate-select-item-indicator" aria-hidden="true">
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 5 5L20 7" /></svg>
                    </span>
                  ) : null}
                </div>
              );
            })}
            {options.filter(function (o) { return matches(o, query()); }).length === 0 ? <div class="krate-select-empty">No results.</div> : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}
