import './visually-hidden.css';

export interface VisuallyHiddenProps {
  children?: any;
  /** Extra classes for the root element. */
  class?: string;
  /** Alias of `class`. */
  className?: string;
  id?: string;
}

export function VisuallyHidden(props: VisuallyHiddenProps) {
  var children = props.children || "";
  return (
    <span class={"krate-visually-hidden" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} id={props.id}>
      {children}
    </span>
  );
}