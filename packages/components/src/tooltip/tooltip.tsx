import './tooltip.css';


export interface TooltipProps {
  children?: any;
}

/**
 * Tooltip. Pure CSS (shows on hover and keyboard focus), so it needs no
 * JavaScript and no signals.
 *
 * ```tsx
 * <Tooltip>
 *   <Tooltip.Trigger>Hover me</Tooltip.Trigger>
 *   <Tooltip.Content>Hint</Tooltip.Content>
 * </Tooltip>
 * ```
 */
export function Tooltip(props: TooltipProps) {
  return <span class="krate-tooltip">{props.children}</span>;
}

export interface TooltipTriggerProps {
  children?: any;
}

/** @deprecated Use `Tooltip.Trigger` instead. */
export function TooltipTrigger(props: TooltipTriggerProps) {
  return (
    <span class="krate-tooltip-trigger" tabIndex={0}>
      {props.children}
    </span>
  );
}

export interface TooltipContentProps {
  children?: any;
}

/** @deprecated Use `Tooltip.Content` instead. */
export function TooltipContent(props: TooltipContentProps) {
  return (
    <span class="krate-tooltip-content" role="tooltip">
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
