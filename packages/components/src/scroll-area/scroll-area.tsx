import './scroll-area.css';

export interface ScrollAreaProps {
  orientation?: 'vertical' | 'horizontal' | 'both';
  maxHeight?: string;
  children?: any;
}

/**
 * Scroll container. Uses the native scrollbar (styled via `scrollbar-*`), so it
 * needs no JavaScript and keyboard scrolling just works.
 */
export function ScrollArea(props: ScrollAreaProps) {
  const orientation = props.orientation || 'vertical';
  return (
    <div
      class={'krate-scroll-area krate-scroll-area-' + orientation}
      style={props.maxHeight ? 'max-height:' + props.maxHeight : undefined}
      tabIndex={0}
    >
      {props.children}
    </div>
  );
}
