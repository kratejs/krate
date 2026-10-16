import './toggle.css';
import { createCSSToggle } from '@krate/runtime';

export interface ToggleProps {
  variant?: 'default' | 'outline';
  size?: 'sm' | 'md' | 'lg';
  children?: any;
}

/**
 * Toggle button. Zero JS: compiles to a hidden checkbox plus `:has()` CSS. The
 * visual pressed state is driven by the inherited `--krate-active` property.
 */
export function Toggle(props: ToggleProps) {
  const [pressed, setPressed] = createCSSToggle(false);
  return (
    <button
      class="krate-toggle"
      data-variant={props.variant || 'default'}
      data-size={props.size || 'md'}
      type="button"
      onClick={() => setPressed(!pressed())}
    >
      {props.children}
    </button>
  );
}
