import './progress.css';
import { createSignal, createEffect } from '@krate/runtime';

export interface ProgressProps {
  value?: number;
  max?: number;
  getValueLabel?: (value: number, max: number) => string;
}

export function Progress(props: ProgressProps) {
  var max = props.max || 100;
  var [value, setValue] = createSignal(props.value || 0);

  createEffect(function () {
    var v = props.value || 0;
    setValue(Math.min(Math.max(v, 0), max));
  });

  return (
    <div class="krate-progress" role="progressbar" aria-valuenow={value()} aria-valuemin={0} aria-valuemax={max}>
      <div class="krate-progress-track">
        <div class="krate-progress-indicator" style={"width: " + (max > 0 ? (value() / max) * 100 : 0) + "%;"}></div>
      </div>
      <span class="krate-progress-label">{props.getValueLabel ? props.getValueLabel(value(), max) : Math.round(max > 0 ? (value() / max) * 100 : 0) + "%"}</span>
    </div>
  );
}
