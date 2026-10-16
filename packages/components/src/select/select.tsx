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
        <option value="" disabled hidden>
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
}

/**
 * Searchable select (command-style). Runs on the Krate runtime signals; use
 * `NativeSelect` when you want zero JavaScript.
 */
export function Select(props: SelectProps) {
  const [open, setOpen] = createSignal(false);
  const [query, setQuery] = createSignal('');
  const [value, setValue] = createSignal(props.defaultValue || '');
  const [text, setText] = createSignal('');
  let root: HTMLElement | null = null;

  function choose(o: SelectOption) {
    if (o.disabled) return;
    setValue(o.value);
    setText(o.label || o.value);
    setOpen(false);
    setQuery('');
  }

  onMount(function () {
    function onDoc(e: MouseEvent) {
      if (root && !root.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', onDoc);
    onCleanup(function () {
      document.removeEventListener('mousedown', onDoc);
    });
  });

  function filtered(): SelectOption[] {
    const q = query().toLowerCase();
    const list = props.options || [];
    if (!q) return list;
    return list.filter(function (o) {
      return (o.label || o.value).toLowerCase().indexOf(q) >= 0 || o.value.toLowerCase().indexOf(q) >= 0;
    });
  }

  return (
    <div class="krate-select" ref={function (el: Element) { root = el as HTMLElement; }}>
      <input type="hidden" name={props.name} value={value()} />
      <button
        class="krate-select-trigger"
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open() ? 'true' : 'false'}
        onClick={function () { setOpen(!open()); }}
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
              placeholder="Search..."
              aria-label="Search options"
              value={query()}
              onInput={function (e: any) { setQuery(e.target.value); }}
            />
          </div>
          <div class="krate-select-viewport" role="listbox">
            {filtered().map(function (o) {
              return (
                <div
                  class={'krate-select-item' + (o.value === value() ? ' krate-select-item-selected' : '') + (o.disabled ? ' krate-select-item-disabled' : '')}
                  role="option"
                  aria-selected={o.value === value() ? 'true' : 'false'}
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
            {filtered().length === 0 ? <div class="krate-select-empty">No results.</div> : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}
