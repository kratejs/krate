import './switch.css';
import { createSignal, createEffect } from '@krate/runtime';

export interface SwitchProps {
  checked?: boolean;
  defaultChecked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
  disabled?: boolean;
  name?: string;
  value?: string;
  id?: string;
}

export default function Switch(props: SwitchProps) {
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
    <button
      class={"krate-switch" + (checked() ? " krate-switch-checked" : "")}
      role="switch"
      type="button"
      aria-checked={checked() ? "true" : "false"}
      data-state={checked() ? "checked" : "unchecked"}
      disabled={props.disabled}
      onClick={toggle}
      onKeyDown={handleKeyDown}
    >
      <span class="krate-switch-thumb" />
    </button>
  );
}
