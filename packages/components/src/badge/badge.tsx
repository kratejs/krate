import './badge.css';

export interface BadgeProps {
  children?: any;
  variant?: 'default' | 'secondary' | 'outline' | 'success' | 'warning' | 'destructive';
  size?: 'sm' | 'md';
}

export function Badge(props: BadgeProps) {
  var variant = props.variant || "default";
  var size = props.size || "md";
  var className = "krate-badge krate-badge-" + variant + " krate-badge-" + size;
  return <span class={className}>{props.children}</span>;
}
