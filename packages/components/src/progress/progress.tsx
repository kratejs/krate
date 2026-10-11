import './progress.css';

export interface ProgressProps {
  value?: number;
  max?: number;
  label?: string;
}

/**
 * Progress indicator. Uses the native `<progress>` element, so it needs no
 * JavaScript and keeps full accessibility (the UA exposes the value).
 */
export function Progress(props: ProgressProps) {
  return (
    <progress
      class="krate-progress"
      value={props.value}
      max={props.max || 100}
      aria-label={props.label}
    />
  );
}
