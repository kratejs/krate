import './badge.css';

export interface BadgeProps {
  children?: any;
  variant?: 'default' | 'secondary' | 'outline' | 'success' | 'warning' | 'destructive';
  size?: 'sm' | 'md';
  /** Extra classes for the root element. */
  class?: string;
  /** Alias of `class`. */
  className?: string;
  id?: string;
}

export function Badge(props: BadgeProps) {
  var variant = props.variant || "default";
  var size = props.size || "md";
  var className = "krate-badge krate-badge-" + variant + " krate-badge-" + size;
  if (props.class) className += " " + props.class;
  if (props.className) className += " " + props.className;
  return <span class={className} id={props.id}>{props.children}</span>;
}
