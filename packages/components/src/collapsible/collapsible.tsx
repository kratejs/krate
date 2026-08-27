import './collapsible.css';
import { createSignal, createEffect } from '@krate/runtime';

export interface CollapsibleProps {
  children?: any;
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
}

export function Collapsible(props: CollapsibleProps) {
  var [open, setOpen] = createSignal(props.defaultOpen || false);

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

  function handleClick(e: MouseEvent) {
    var trigger = (e.target as HTMLElement).closest("[data-krate-collapsible-trigger]");
    if (trigger) {
      toggle();
    }
  }

  return (
    <div class="krate-collapsible" data-state={open() ? "open" : "closed"} onClick={handleClick}>
      {props.children}
    </div>
  );
}

export interface CollapsibleTriggerProps {
  children?: any;
}

export function CollapsibleTrigger(props: CollapsibleTriggerProps) {
  return (
    <div class="krate-collapsible-trigger" data-krate-collapsible-trigger="true">
      {props.children}
    </div>
  );
}

export interface CollapsibleContentProps {
  children?: any;
}

export function CollapsibleContent(props: CollapsibleContentProps) {
  return (
    <div class="krate-collapsible-content" data-krate-collapsible-content="true">
      <div class="krate-collapsible-content-inner">
        {props.children}
      </div>
    </div>
  );
}
