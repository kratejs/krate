import './popover.css';
import { createSignal, createEffect, onCleanup } from '@krate/runtime';

export interface PopoverProps {
  children?: any;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}

export default function Popover(props: PopoverProps) {
  var [open, setOpen] = createSignal(false);
  var rootRef: HTMLElement | null = null;

  createEffect(function () {
    if (props.open !== undefined) {
      setOpen(props.open);
    }
  });

  function toggle() {
    var next = !open();
    setOpen(next);
    if (props.onOpenChange) {
      props.onOpenChange(next);
    }
  }

  function close() {
    setOpen(false);
    if (props.onOpenChange) {
      props.onOpenChange(false);
    }
  }

  function handleClick(e: MouseEvent) {
    var target = e.target as HTMLElement;
    var root = target.closest("[data-krate-popover]") as HTMLElement;
    if (root) rootRef = root;

    var trigger = target.closest("[data-krate-popover-trigger]");
    if (trigger) {
      toggle();
      return;
    }
  }

  function handleClickOutside(e: MouseEvent) {
    var r = rootRef;
    if (r && !r.contains(e.target as Node)) {
      close();
    }
  }

  createEffect(function () {
    var outsideTimer: any = null;
    if (open()) {
      outsideTimer = setTimeout(function () {
        document.addEventListener("click", handleClickOutside);
      }, 0);
    }
    return function () {
      if (outsideTimer) clearTimeout(outsideTimer);
      document.removeEventListener("click", handleClickOutside);
    };
  });

  return (
    <div class="krate-popover" data-krate-popover="true" data-state={open() ? "open" : "closed"} onClick={handleClick}>
      {props.children}
    </div>
  );
}

export interface PopoverTriggerProps {
  children?: any;
}

export function PopoverTrigger(props: PopoverTriggerProps) {
  return (
    <button class="krate-popover-trigger" type="button" aria-haspopup="dialog" data-krate-popover-trigger="true">
      {props.children}
    </button>
  );
}

export interface PopoverContentProps {
  children?: any;
  align?: 'start' | 'center' | 'end';
  side?: 'top' | 'bottom';
  sideOffset?: number;
}

export function PopoverContent(props: PopoverContentProps) {
  var children = props.children || "";
  var align = props.align || "center";
  var side = props.side || "bottom";

  var className = "krate-popover-content krate-popover-content-" + side;
  if (align === "end") className += " krate-popover-align-end";
  else if (align === "start") className += " krate-popover-align-start";

  return (
    <div class={className} role="dialog" aria-modal="true" data-krate-popover-content="true">
      {children}
    </div>
  );
}
