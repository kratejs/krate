import './visually-hidden.css';

export interface VisuallyHiddenProps {
  children?: any;
}

export default function VisuallyHidden(props: VisuallyHiddenProps) {
  var children = props.children || "";
  return (
    <span class="krate-visually-hidden">
      {children}
    </span>
  );
}