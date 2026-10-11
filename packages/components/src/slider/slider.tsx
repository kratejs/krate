import './slider.css';

export interface SliderProps {
  min?: number;
  max?: number;
  step?: number;
  defaultValue?: number;
  label?: string;
  name?: string;
}

/**
 * Slider. Uses the native `<input type="range">`, so it needs no JavaScript and
 * keeps full keyboard/assistive support and form submission.
 */
export function Slider(props: SliderProps) {
  return (
    <input
      type="range"
      class="krate-slider"
      min={props.min === undefined ? 0 : props.min}
      max={props.max === undefined ? 100 : props.max}
      step={props.step === undefined ? 1 : props.step}
      value={props.defaultValue === undefined ? 0 : props.defaultValue}
      name={props.name}
      aria-label={props.label}
    />
  );
}
