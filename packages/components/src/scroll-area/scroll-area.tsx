import './scroll-area.css';

export interface ScrollAreaProps {
  orientation?: 'vertical' | 'horizontal' | 'both';
  maxHeight?: string;
  children?: any;
  /** Accessible name; the area is a focusable region so keyboard users can scroll it. */
  label?: string;
  /** Extra classes for the root element. */
  class?: string;
  /** Alias of `class`. */
  className?: string;
  id?: string;
}

/**
 * Scroll container. Uses the native scrollbar (styled via `scrollbar-*`), so it
 * needs no JavaScript and keyboard scrolling just works.
 */
export function ScrollArea(props: ScrollAreaProps) {
  const orientation = props.orientation || 'vertical';
  return (
    <div
      class={'krate-scroll-area krate-scroll-area-' + orientation + (props.class ? ' ' + props.class : '') + (props.className ? ' ' + props.className : '')}
      id={props.id}
      role="region"
      aria-label={props.label || 'Scrollable content'}
      style={props.maxHeight ? 'max-height:' + props.maxHeight : undefined}
      tabIndex={0}
    >
      {props.children}
    </div>
  );
}
