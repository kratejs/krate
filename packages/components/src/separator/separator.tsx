import './separator.css';

export interface SeparatorProps {
  orientation?: 'horizontal' | 'vertical';
  decorative?: boolean;
}

export default function Separator(props: SeparatorProps) {
  var orientation = props.orientation || "horizontal";

  return (
    <div
      class={"krate-separator krate-separator-" + orientation}
      role="separator"
      aria-orientation={orientation}
      data-orientation={orientation}
    />
  );
}
