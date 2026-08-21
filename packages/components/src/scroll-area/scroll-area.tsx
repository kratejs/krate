import './scroll-area.css';
import { createSignal, onMount, onCleanup } from '@krate/runtime';

export interface ScrollAreaProps {
  children?: any;
  orientation?: 'vertical' | 'horizontal' | 'both';
  maxHeight?: string;
  scrollbarSize?: number;
}

export default function ScrollArea(props: ScrollAreaProps) {
  var orientation = props.orientation || "vertical";
  var maxHeight = props.maxHeight || "300px";
  var scrollbarSize = props.scrollbarSize || 8;

  return (
    <div
      class="krate-scroll-area"
      style={"max-height: " + maxHeight + ";"}
    >
      <div class="krate-scroll-area-viewport">
        {props.children}
      </div>
      <div class={"krate-scroll-area-scrollbar krate-scroll-area-scrollbar-" + orientation}>
        <div class="krate-scroll-area-thumb"></div>
      </div>
    </div>
  );
}

export interface ScrollAreaViewportProps {
  children?: any;
}

export function ScrollAreaViewport(props: ScrollAreaViewportProps) {
  return (
    <div class="krate-scroll-area-viewport">
      {props.children}
    </div>
  );
}

export interface ScrollAreaScrollbarProps {
  orientation?: 'vertical' | 'horizontal';
}

export function ScrollAreaScrollbar(props: ScrollAreaScrollbarProps) {
  var orientation = props.orientation || "vertical";
  return (
    <div class={"krate-scroll-area-scrollbar krate-scroll-area-scrollbar-" + orientation}>
      <div class="krate-scroll-area-thumb"></div>
    </div>
  );
}

export interface ScrollAreaThumbProps {
}

export function ScrollAreaThumb(props: ScrollAreaThumbProps) {
  return <div class="krate-scroll-area-thumb" />;
}
