import './slider.css';

export interface SliderProps {
  min?: number;
  max?: number;
  step?: number;
  defaultValue?: number;
  label?: string;
  name?: string;
  disabled?: boolean;
  /** Pair with `<Label htmlFor>`. */
  id?: string;
  /**
   * Runs when the value is committed (the handle is released, or a key press
   * lands). Opt-in: a Slider that passes no handler stays script-free.
   */
  onChange?: (e: Event) => void;
  /** Runs on every value change while the handle moves. Opt-in like onChange. */
  onInput?: (e: Event) => void;
  class?: string;
  className?: string;
}

/**
 * Slider. Uses the native `<input type="range">`, so it needs no JavaScript and
 * keeps full keyboard/assistive support and form submission. Passing `onChange`
 * or `onInput` hydrates that one slider; the others stay static.
 */
export function Slider(props: SliderProps) {
  return (
    <input
      type="range"
      class={"krate-slider" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}
      id={props.id}
      disabled={props.disabled}
      min={props.min === undefined ? 0 : props.min}
      max={props.max === undefined ? 100 : props.max}
      step={props.step === undefined ? 1 : props.step}
      value={props.defaultValue === undefined ? 0 : props.defaultValue}
      name={props.name}
      onChange={props.onChange}
      onInput={props.onInput}
      aria-label={props.label}
    />
  );
}
