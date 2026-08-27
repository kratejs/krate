import './toggle.css';
import { createSignal, createEffect } from '@krate/runtime';

export interface ToggleProps {
  pressed?: boolean;
  defaultPressed?: boolean;
  onPressedChange?: (pressed: boolean) => void;
  disabled?: boolean;
  variant?: 'default' | 'outline';
  size?: 'sm' | 'md' | 'lg';
  children?: any;
}

export function Toggle(props: ToggleProps) {
  var [pressed, setPressed] = createSignal(props.defaultPressed || false);

  createEffect(function () {
    if (props.pressed !== undefined) {
      setPressed(props.pressed);
    }
  });

  function toggle() {
    if (props.disabled) return;
    var next = !pressed();
    setPressed(next);
    if (props.onPressedChange) {
      props.onPressedChange(next);
    }
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === " " || e.key === "Enter") {
      e.preventDefault();
      toggle();
    }
  }

  var className = "krate-toggle";
  if (props.variant === "outline") className += " krate-toggle-outline";
  if (props.size === "sm") className += " krate-toggle-sm";
  else if (props.size === "lg") className += " krate-toggle-lg";

  return (
    <button
      class={className}
      type="button"
      aria-pressed={pressed() ? "true" : "false"}
      data-state={pressed() ? "on" : "off"}
      disabled={props.disabled}
      onClick={toggle}
      onKeyDown={handleKeyDown}
    >
      {props.children}
    </button>
  );
}
