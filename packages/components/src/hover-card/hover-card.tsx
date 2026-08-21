import './hover-card.css';
import { createSignal, createEffect, onCleanup } from '@krate/runtime';

export interface HoverCardProps {
  children?: any;
  openDelay?: number;
  closeDelay?: number;
}

export default function HoverCard(props: HoverCardProps) {
  var openDelay = props.openDelay || 200;
  var closeDelay = props.closeDelay || 150;
  var [open, setOpen] = createSignal(false);
  var openTimer: any = null;
  var closeTimer: any = null;

  function handleMouseOver(e: MouseEvent) {
    var related = e.relatedTarget as HTMLElement;
    var root = (e.target as HTMLElement).closest("[data-krate-hover-card]") as HTMLElement;
    if (!root) return;
    if (related && root.contains(related)) return;
    var oDelay = Number(root.getAttribute("data-open-delay")) || 200;
    clearTimeout(closeTimer);
    openTimer = setTimeout(function () {
      setOpen(true);
    }, oDelay);
  }

  function handleMouseOut(e: MouseEvent) {
    var related = e.relatedTarget as HTMLElement;
    var root = (e.target as HTMLElement).closest("[data-krate-hover-card]") as HTMLElement;
    if (!root) return;
    if (related && root.contains(related)) return;
    var cDelay = Number(root.getAttribute("data-close-delay")) || 150;
    clearTimeout(openTimer);
    closeTimer = setTimeout(function () {
      setOpen(false);
    }, cDelay);
  }

  onCleanup(function () {
    clearTimeout(openTimer);
    clearTimeout(closeTimer);
  });

  return (
    <div
      class="krate-hover-card"
      data-krate-hover-card="true"
      data-state={open() ? "open" : "closed"}
      data-open-delay={openDelay}
      data-close-delay={closeDelay}
      onMouseOver={handleMouseOver}
      onMouseOut={handleMouseOut}
    >
      {props.children}
    </div>
  );
}

export interface HoverCardTriggerProps {
  children?: any;
}

export function HoverCardTrigger(props: HoverCardTriggerProps) {
  return (
    <div class="krate-hover-card-trigger">
      {props.children}
    </div>
  );
}

export interface HoverCardContentProps {
  children?: any;
  align?: 'start' | 'center' | 'end';
  side?: 'top' | 'bottom';
}

export function HoverCardContent(props: HoverCardContentProps) {
  var children = props.children || "";
  var align = props.align || "center";
  var side = props.side || "bottom";

  var className = "krate-hover-card-content krate-hover-card-content-" + side;
  if (align === "end") className += " krate-hover-card-align-end";
  else if (align === "center") className += " krate-hover-card-align-center";

  return (
    <div class={className}>
      {children}
    </div>
  );
}
