import './switch.css';
import '../visually-hidden/visually-hidden.css';
import { createCSSToggle } from '@krate/runtime';

export interface SwitchProps {
  /** Accessible label for the switch. */
  label?: string;
  children?: any;
}

/**
 * Switch. Zero JS: compiles to a native checkbox (role=switch, named by the
 * label text, keyboard operable with Space) plus `:has()` CSS. The initial
 * state is fixed at build time (uncontrolled); use the CSS-signal `part` API if
 * you need a per-instance initial value.
 */
export function Switch(props: SwitchProps) {
  const [on, setOn] = createCSSToggle(false, { as: 'switch' });
  return (
    <button class="krate-switch" type="button" onClick={() => setOn(!on())}>
      <span class="krate-switch-thumb" />
      <span class="krate-visually-hidden">{props.label}</span>
      {props.children}
    </button>
  );
}
