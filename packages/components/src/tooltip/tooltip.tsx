import './tooltip.css';
import { createContext, onMount, Slot } from '@krate/runtime';

const TooltipCtx = createContext<string>('');

export interface TooltipProps {
  children?: any;
  /** Optional id for the content. Without it the trigger is linked to the content when the page loads. */
  id?: string;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/**
 * Tooltip. Shows on hover and keyboard focus with CSS. The trigger is linked to
 * the tip with `aria-describedby`, Escape hides it while the pointer or focus
 * is on it, and the pointer can move onto the tip without it disappearing.
 *
 * ```tsx
 * <Tooltip>
 *   <Tooltip.Trigger>Hover me</Tooltip.Trigger>
 *   <Tooltip.Content>Hint</Tooltip.Content>
 * </Tooltip>
 * ```
 *
 * To describe a real button or link, use `<Tooltip.Trigger asChild>` around it.
 */
export function Tooltip(props: TooltipProps) {
  var root: HTMLElement | null = null;
  var given = props.id || "";

  // Without an explicit `id`, link the trigger to the content on mount: the
  // ids must be unique per instance and are not known when the page is built.
  onMount(function () {
    if (!root) return;
    var trigger = root.querySelector(".krate-tooltip-trigger") as HTMLElement | null;
    var content = root.querySelector(".krate-tooltip-content") as HTMLElement | null;
    if (!trigger || !content) return;
    if (!content.id) content.id = "krate-tooltip-" + Math.random().toString(36).slice(2, 8);
    trigger.setAttribute("aria-describedby", content.id);
  });

  // Escape dismisses the tip even when only the pointer is over it, so listen
  // on the document while the pointer is inside.
  function onEnter(e: Event) {
    var root = e.currentTarget as HTMLElement;
    root.removeAttribute("data-dismissed");
    var onKey = function (ev: KeyboardEvent) {
      if (ev.key === "Escape") root.setAttribute("data-dismissed", "true");
    };
    document.addEventListener("keydown", onKey);
    (root as any).__krateTipKey = onKey;
  }

  function onLeave(e: Event) {
    var root = e.currentTarget as HTMLElement;
    var onKey = (root as any).__krateTipKey;
    if (onKey) document.removeEventListener("keydown", onKey);
    (root as any).__krateTipKey = null;
    root.removeAttribute("data-dismissed");
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape") (e.currentTarget as HTMLElement).setAttribute("data-dismissed", "true");
  }

  function onBlur(e: FocusEvent) {
    var root = e.currentTarget as HTMLElement;
    if (!root.contains(e.relatedTarget as Node | null)) root.removeAttribute("data-dismissed");
  }

  return (
    <TooltipCtx.Provider value={given}>
      <span
        class={"krate-tooltip" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}
        ref={function (el: Element) { root = el as HTMLElement; }}
        onMouseEnter={onEnter}
        onMouseLeave={onLeave}
        onKeyDown={onKeyDown}
        onBlur={onBlur}
      >
        {props.children}
      </span>
    </TooltipCtx.Provider>
  );
}

export interface TooltipTriggerProps {
  children?: any;
  /** Describe the single child element (a button or link) instead of wrapping it in a focusable span. */
  asChild?: boolean;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Tooltip.Trigger` instead. */
export function TooltipTrigger(props: TooltipTriggerProps) {
  var id = TooltipCtx.use();
  var className = "krate-tooltip-trigger";
  if (props.class) className += " " + props.class;
  if (props.className) className += " " + props.className;
  if (props.asChild) {
    return <Slot class={className} aria-describedby={id || undefined}>{props.children}</Slot>;
  }
  return (
    <span class={className} tabIndex={0} aria-describedby={id || undefined}>
      {props.children}
    </span>
  );
}

export interface TooltipContentProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Tooltip.Content` instead. */
export function TooltipContent(props: TooltipContentProps) {
  var id = TooltipCtx.use();
  return (
    <span class={"krate-tooltip-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} role="tooltip" id={id || undefined}>
      {props.children}
    </span>
  );
}

export interface Tooltip {
  Trigger: typeof TooltipTrigger;
  Content: typeof TooltipContent;
}

Tooltip.Trigger = TooltipTrigger;
Tooltip.Content = TooltipContent;
