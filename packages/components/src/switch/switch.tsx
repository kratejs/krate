import './switch.css';
import '../visually-hidden/visually-hidden.css';

export interface SwitchProps {
  /** Accessible label for the switch. */
  label?: string;
  children?: any;
  /** Name the switch is submitted under in a form. */
  name?: string;
  /** Submitted value when on (default "on"). */
  value?: string;
  /** Initial state. The switch is uncontrolled: the browser owns it afterwards. */
  defaultChecked?: boolean;
  disabled?: boolean;
  required?: boolean;
  /** Put on the input, so `<Label htmlFor>` can point at it. */
  id?: string;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/**
 * Switch. Zero JS: a native checkbox with role=switch inside a label, styled
 * with `:checked`. It submits with a form under `name`/`value`, is named by the
 * label text and operable with Space.
 */
export function Switch(props: SwitchProps) {
  return (
    <label class={"krate-switch" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>
      <input
        class="krate-switch-input"
        type="checkbox"
        role="switch"
        id={props.id}
        name={props.name}
        value={props.value}
        defaultChecked={props.defaultChecked}
        disabled={props.disabled}
        required={props.required}
      />
      <span class="krate-switch-thumb" />
      <span class="krate-visually-hidden">{props.label}</span>
      {props.children}
    </label>
  );
}
