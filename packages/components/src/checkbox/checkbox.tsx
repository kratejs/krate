import './checkbox.css';
import { createSignal, createEffect } from '@krate/runtime';

export interface CheckboxProps {
  checked?: boolean;
  defaultChecked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
  disabled?: boolean;
  required?: boolean;
  name?: string;
  value?: string;
  id?: string;
  children?: any;
}

export function Checkbox(props: CheckboxProps) {
  var [checked, setChecked] = createSignal(props.defaultChecked || false);

  createEffect(function () {
    if (props.checked !== undefined) {
      setChecked(props.checked);
    }
  });

  function toggle() {
    if (props.disabled) return;
    var next = !checked();
    setChecked(next);
    if (props.onCheckedChange) {
      props.onCheckedChange(next);
    }
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === " " || e.key === "Enter") {
      e.preventDefault();
      toggle();
    }
  }

  return (
    <span class="krate-checkbox-wrapper">
      <button
        class={"krate-checkbox" + (checked() ? " krate-checkbox-checked" : "")}
        role="checkbox"
        type="button"
        aria-checked={checked() ? "true" : "false"}
        data-state={checked() ? "checked" : "unchecked"}
        disabled={props.disabled}
        id={props.id || ""}
        name={props.name || ""}
        value={props.value || ""}
        required={props.required}
        onClick={toggle}
        onKeyDown={handleKeyDown}
      >
        <span class="krate-checkbox-indicator">
          {checked() ? (
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="20 6 9 17 4 12"/>
            </svg>
          ) : null}
        </span>
      </button>
      {props.children ? (
        <label class="krate-checkbox-label" onClick={toggle}>
          {props.children}
        </label>
      ) : null}
    </span>
  );
}
