import './slider.css';
import { createSignal, createEffect, onMount, onCleanup } from '@krate/runtime';

export interface SliderProps {
  value?: number;
  defaultValue?: number;
  min?: number;
  max?: number;
  step?: number;
  onValueChange?: (value: number) => void;
  disabled?: boolean;
  orientation?: 'horizontal' | 'vertical';
}

export default function Slider(props: SliderProps) {
  var min = props.min || 0;
  var max = props.max || 100;
  var step = props.step || 1;
  var [value, setValue] = createSignal(props.defaultValue || min);
  var [dragging, setDragging] = createSignal(false);
  var rootRef: HTMLElement | null = null;

  createEffect(function () {
    if (props.value !== undefined) {
      setValue(props.value);
    }
  });

  function calculateValueFromEvent(e: MouseEvent) {
    var root = rootRef;
    if (!root) return value();
    var track = root.querySelector(".krate-slider-track") as HTMLElement;
    if (!track) return value();
    var rMin = Number(root.getAttribute("data-min")) || 0;
    var rMax = Number(root.getAttribute("data-max")) || 100;
    var rStep = Number(root.getAttribute("data-step")) || 1;
    var rect = track.getBoundingClientRect();
    var diff = e.clientX - rect.left;
    var width = rect.width;
    var pct = diff / width;
    var range = rMax - rMin;
    var raw = rMin + pct * range;
    var stepped = Math.round(raw / rStep) * rStep;
    return Math.min(Math.max(stepped, rMin), rMax);
  }

  function handleTrackClick(e: MouseEvent) {
    var root = (e.target as HTMLElement).closest("[data-krate-slider]") as HTMLElement;
    if (!root) return;
    rootRef = root;
    var clamped = calculateValueFromEvent(e);
    setValue(clamped);
    if (props.onValueChange) props.onValueChange(clamped);
  }

  function handleThumbMouseDown(e: MouseEvent) {
    if (props.disabled) return;
    var root = (e.target as HTMLElement).closest("[data-krate-slider]") as HTMLElement;
    if (root) rootRef = root;
    setDragging(true);
    e.preventDefault();
  }

  createEffect(function () {
    if (!dragging()) return;

    function handleMouseMove(e: MouseEvent) {
      var newValue = calculateValueFromEvent(e);
      setValue(newValue);
      if (props.onValueChange) props.onValueChange(newValue);
    }

    function handleMouseUp() {
      setDragging(false);
    }

    document.addEventListener("mousemove", handleMouseMove);
    document.addEventListener("mouseup", handleMouseUp);

    return function () {
      document.removeEventListener("mousemove", handleMouseMove);
      document.removeEventListener("mouseup", handleMouseUp);
    };
  });

  function handleKeyDown(e: KeyboardEvent) {
    var root = (e.target as HTMLElement).closest("[data-krate-slider]") as HTMLElement;
    if (!root) return;
    var rMin = Number(root.getAttribute("data-min")) || 0;
    var rMax = Number(root.getAttribute("data-max")) || 100;
    var rStep = Number(root.getAttribute("data-step")) || 1;
    var current = value();
    var next = current;
    if (e.key === "ArrowRight" || e.key === "ArrowUp") {
      next = Math.min(current + rStep, rMax);
    } else if (e.key === "ArrowLeft" || e.key === "ArrowDown") {
      next = Math.max(current - rStep, rMin);
    } else if (e.key === "Home") {
      next = rMin;
    } else if (e.key === "End") {
      next = rMax;
    } else {
      return;
    }
    e.preventDefault();
    setValue(next);
    if (props.onValueChange) props.onValueChange(next);
  }

  return (
    <div
      class={"krate-slider" + (props.disabled ? " krate-slider-disabled" : "")}
      data-krate-slider="true"
      data-orientation={props.orientation || "horizontal"}
      data-min={min}
      data-max={max}
      data-step={step}
    >
      <div
        class="krate-slider-track"
        onClick={handleTrackClick}
      >
        <div class="krate-slider-range" style={"width: " + ((value() - min) / (max - min)) * 100 + "%;"}></div>
        <div
          class="krate-slider-thumb"
          style={"left: " + ((value() - min) / (max - min)) * 100 + "%;"}
          role="slider"
          tabindex={props.disabled ? -1 : 0}
          aria-valuemin={min}
          aria-valuemax={max}
          aria-valuenow={value()}
          onMouseDown={handleThumbMouseDown}
          onKeyDown={handleKeyDown}
        />
      </div>
    </div>
  );
}
