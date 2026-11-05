import './hover-card.css';
import { createContext, onMount, Slot } from '@krate/runtime';

const HoverCardCtx = createContext<string>('');

export interface HoverCardProps {
  children?: any;
  /** Optional id for the content. Without it the trigger is linked to the content when the page loads. */
  id?: string;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/**
 * Hover card. Shows on hover and keyboard focus with CSS. The trigger is linked
 * to the card with `aria-describedby`, and Escape hides the card while the
 * pointer or focus is on it.
 *
 * ```tsx
 * <HoverCard>
 *   <HoverCard.Trigger>@user</HoverCard.Trigger>
 *   <HoverCard.Content>Profile summary</HoverCard.Content>
 * </HoverCard>
 * ```
 *
 * To attach the card to a real link, use `<HoverCard.Trigger asChild>` around it.
 */
export function HoverCard(props: HoverCardProps) {
  var root: HTMLElement | null = null;
  var given = props.id || "";

  // Without an explicit `id`, link the trigger to the content on mount: the
  // ids must be unique per instance and are not known when the page is built.
  onMount(function () {
    if (!root) return;
    var trigger = root.querySelector(".krate-hover-card-trigger") as HTMLElement | null;
    var content = root.querySelector(".krate-hover-card-content") as HTMLElement | null;
    if (!trigger || !content) return;
    if (!content.id) content.id = "krate-hover-card-" + Math.random().toString(36).slice(2, 8);
    trigger.setAttribute("aria-describedby", content.id);
  });

  function onEnter(e: Event) {
    var root = e.currentTarget as HTMLElement;
    root.removeAttribute("data-dismissed");
    var onKey = function (ev: KeyboardEvent) {
      if (ev.key === "Escape") root.setAttribute("data-dismissed", "true");
    };
    document.addEventListener("keydown", onKey);
    (root as any).__krateCardKey = onKey;
  }

  function onLeave(e: Event) {
    var root = e.currentTarget as HTMLElement;
    var onKey = (root as any).__krateCardKey;
    if (onKey) document.removeEventListener("keydown", onKey);
    (root as any).__krateCardKey = null;
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
    <HoverCardCtx.Provider value={given}>
      <span
        class={"krate-hover-card" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}
        ref={function (el: Element) { root = el as HTMLElement; }}
        onMouseEnter={onEnter}
        onMouseLeave={onLeave}
        onKeyDown={onKeyDown}
        onBlur={onBlur}
      >
        {props.children}
      </span>
    </HoverCardCtx.Provider>
  );
}

export interface HoverCardTriggerProps {
  children?: any;
  /** Attach the card to the single child element (a link or button) instead of a focusable span. */
  asChild?: boolean;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `HoverCard.Trigger` instead. */
export function HoverCardTrigger(props: HoverCardTriggerProps) {
  var id = HoverCardCtx.use();
  var className = "krate-hover-card-trigger";
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

export interface HoverCardContentProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `HoverCard.Content` instead. */
export function HoverCardContent(props: HoverCardContentProps) {
  var id = HoverCardCtx.use();
  return (
    <span class={"krate-hover-card-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} role="group" id={id || undefined}>
      {props.children}
    </span>
  );
}

export interface HoverCard {
  Trigger: typeof HoverCardTrigger;
  Content: typeof HoverCardContent;
}

HoverCard.Trigger = HoverCardTrigger;
HoverCard.Content = HoverCardContent;
