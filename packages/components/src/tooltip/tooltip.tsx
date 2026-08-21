import './tooltip.css';
import { createSignal, createEffect, onCleanup } from '@krate/runtime';

export interface TooltipProps {
  children?: any;
}

export default function Tooltip(props: TooltipProps) {
  var [open, setOpen] = createSignal(false);
  var hoverTimer: any = null;

  function handleMouseOver(e: MouseEvent) {
    var related = e.relatedTarget as HTMLElement;
    var root = (e.target as HTMLElement).closest("[data-krate-tooltip-root]") as HTMLElement;
    if (!root) return;
    if (related && root.contains(related)) return;
    hoverTimer = setTimeout(function () {
      setOpen(true);
    }, 200);
  }

  function handleMouseOut(e: MouseEvent) {
    var related = e.relatedTarget as HTMLElement;
    var root = (e.target as HTMLElement).closest("[data-krate-tooltip-root]") as HTMLElement;
    if (!root) return;
    if (related && root.contains(related)) return;
    if (hoverTimer) {
      clearTimeout(hoverTimer);
      hoverTimer = null;
    }
    setOpen(false);
  }

  onCleanup(function () {
    if (hoverTimer) clearTimeout(hoverTimer);
  });

  return (
    <div
      class="krate-tooltip-root"
      data-state={open() ? "open" : "closed"}
      data-krate-tooltip-root="true"
      onMouseOver={handleMouseOver}
      onMouseOut={handleMouseOut}
    >
      {props.children}
    </div>
  );
}

export interface TooltipTriggerProps {
  children?: any;
}

export function TooltipTrigger(props: TooltipTriggerProps) {
  return (
    <div class="krate-tooltip-trigger">
      {props.children}
    </div>
  );
}

export interface TooltipContentProps {
  children?: any;
  side?: 'top' | 'bottom' | 'left' | 'right';
  sideOffset?: number;
}

export function TooltipContent(props: TooltipContentProps) {
  var side = props.side || "top";
  var sideOffset = props.sideOffset || 4;

  var className = "krate-tooltip-content krate-tooltip-content-" + side;

  return (
    <div class={className} style={"--tooltip-offset: " + sideOffset + "px;"}>
      {props.children}
      <div class="krate-tooltip-arrow" />
    </div>
  );
}
