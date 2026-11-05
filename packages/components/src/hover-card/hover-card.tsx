import './hover-card.css';


export interface HoverCardProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/**
 * Hover card. Pure CSS (shows on hover and keyboard focus); no JS, no signals.
 *
 * ```tsx
 * <HoverCard>
 *   <HoverCard.Trigger>@user</HoverCard.Trigger>
 *   <HoverCard.Content>Profile summary</HoverCard.Content>
 * </HoverCard>
 * ```
 */
export function HoverCard(props: HoverCardProps) {
  return <span class={"krate-hover-card" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>{props.children}</span>;
}

export interface HoverCardTriggerProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `HoverCard.Trigger` instead. */
export function HoverCardTrigger(props: HoverCardTriggerProps) {
  return (
    <span class={"krate-hover-card-trigger" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} tabIndex={0}>
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
  return <span class={"krate-hover-card-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>{props.children}</span>;
}

export interface HoverCard {
  Trigger: typeof HoverCardTrigger;
  Content: typeof HoverCardContent;
}

HoverCard.Trigger = HoverCardTrigger;
HoverCard.Content = HoverCardContent;
