import './separator.css';

export interface SeparatorProps {
  orientation?: 'horizontal' | 'vertical';
  /** Purely visual: exposes no separator role to assistive technology. */
  decorative?: boolean;
  style?: string;
  /** Extra classes for the root element. */
  class?: string;
  /** Alias of `class`. */
  className?: string;
  id?: string;
}

export function Separator(props: SeparatorProps) {
  var orientation = props.orientation || "horizontal";

  return (
    <div
      class={"krate-separator krate-separator-" + orientation + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}
      id={props.id}
      role={props.decorative ? "none" : "separator"}
      aria-orientation={props.decorative ? undefined : orientation}
      style={props.style}
      data-orientation={orientation}
    />
  );
}
