import './separator.css';

export interface SeparatorProps {
  orientation?: 'horizontal' | 'vertical';
  /** Purely visual: exposes no separator role to assistive technology. */
  decorative?: boolean;
  style?: string;
}

export function Separator(props: SeparatorProps) {
  var orientation = props.orientation || "horizontal";

  return (
    <div
      class={"krate-separator krate-separator-" + orientation}
      role={props.decorative ? "none" : "separator"}
      aria-orientation={props.decorative ? undefined : orientation}
      style={props.style}
      data-orientation={orientation}
    />
  );
}
