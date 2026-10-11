import './checkbox.css';
import { createCSSToggle } from '@krate/runtime';

export interface CheckboxProps {
  children?: any;
}

/**
 * Checkbox. Zero JS: compiles to a hidden checkbox plus `:has()` CSS. The
 * initial state is fixed at build time (uncontrolled); use a native
 * `<input type="checkbox">` when you need a submitted, per-instance value.
 */
export function Checkbox(props: CheckboxProps) {
  const [checked, setChecked] = createCSSToggle(false);
  return (
    <button class="krate-checkbox" type="button" onClick={() => setChecked(!checked())}>
      <span class="krate-checkbox-box" aria-hidden="true">
        <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
          <path d="m5 12 5 5L20 7" />
        </svg>
      </span>
      <span class="krate-checkbox-label">{props.children}</span>
    </button>
  );
}
