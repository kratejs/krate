import './checkbox.css';

export interface CheckboxProps {
  children?: any;
  /** Name the checkbox is submitted under in a form. */
  name?: string;
  /** Submitted value when checked (default "on"). */
  value?: string;
  /** Initial state. The checkbox is uncontrolled: the browser owns it afterwards. */
  defaultChecked?: boolean;
  disabled?: boolean;
  required?: boolean;
  /** Put on the input, so `<Label htmlFor>` and form errors can point at it. */
  id?: string;
  /** Extra classes for the wrapping label. */
  class?: string;
  /** Alias of `class`. */
  className?: string;
  /** Accessible name when there is no visible label text. */
  label?: string;
}

/**
 * Checkbox. Zero JS: a native `<input type="checkbox">` inside a label, styled
 * with `:checked`. It submits with a form under `name`/`value`, is operable with
 * Space, and works with `<Label htmlFor>` through `id`.
 */
export function Checkbox(props: CheckboxProps) {
  return (
    <label class={"krate-checkbox" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>
      <input
        class="krate-checkbox-input"
        type="checkbox"
        id={props.id}
        name={props.name}
        value={props.value}
        defaultChecked={props.defaultChecked}
        disabled={props.disabled}
        required={props.required}
        aria-label={props.label}
      />
      <span class="krate-checkbox-box" aria-hidden="true">
        <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
          <path d="m5 12 5 5L20 7" />
        </svg>
      </span>
      <span class="krate-checkbox-label">{props.children}</span>
    </label>
  );
}
