import './hover-card.css';


export interface HoverCardProps {
  children?: any;
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
  return <span class="krate-hover-card">{props.children}</span>;
}

export interface HoverCardTriggerProps {
  children?: any;
}

/** @deprecated Use `HoverCard.Trigger` instead. */
export function HoverCardTrigger(props: HoverCardTriggerProps) {
  return (
    <span class="krate-hover-card-trigger" tabIndex={0}>
      {props.children}
    </span>
  );
}

export interface HoverCardContentProps {
  children?: any;
}

/** @deprecated Use `HoverCard.Content` instead. */
export function HoverCardContent(props: HoverCardContentProps) {
  return <span class="krate-hover-card-content">{props.children}</span>;
}

export interface HoverCard {
  Trigger: typeof HoverCardTrigger;
  Content: typeof HoverCardContent;
}

HoverCard.Trigger = HoverCardTrigger;
HoverCard.Content = HoverCardContent;
