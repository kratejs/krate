import './progress.css';

export interface ProgressProps {
  value?: number;
  max?: number;
  label?: string;
  /** Extra classes for the root element. */
  class?: string;
  /** Alias of `class`. */
  className?: string;
  id?: string;
}

/**
 * Progress indicator. Uses the native `<progress>` element, so it needs no
 * JavaScript and keeps full accessibility (the UA exposes the value).
 */
export function Progress(props: ProgressProps) {
  return (
    <progress
      class={"krate-progress" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}
      id={props.id}
      value={props.value}
      max={props.max || 100}
      aria-label={props.label}
    />
  );
}
