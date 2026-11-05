import './tooltip.css';


export interface TooltipProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
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
  return <span class={"krate-tooltip" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>{props.children}</span>;
}

export interface TooltipTriggerProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `Tooltip.Trigger` instead. */
export function TooltipTrigger(props: TooltipTriggerProps) {
  return (
    <span class={"krate-tooltip-trigger" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} tabIndex={0}>
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
  return (
    <span class={"krate-tooltip-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} role="tooltip">
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
