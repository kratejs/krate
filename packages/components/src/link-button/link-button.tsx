import './link-button.css';

export interface LinkButtonProps {
  children?: any;
  href?: string;
  variant?: 'primary' | 'secondary' | 'ghost' | 'destructive';
  size?: 'sm' | 'md' | 'lg';
  icon?: string;
  iconPosition?: 'left' | 'right';
  /** Extra classes for the root element. */
  class?: string;
  /** Alias of `class`. */
  className?: string;
  id?: string;
}

export function LinkButton(props: LinkButtonProps) {
  var children = props.children || "";
  var href = props.href || "#";
  var variant = props.variant || "primary";
  var size = props.size || "md";
  var icon = props.icon || "";
  var iconPosition = props.iconPosition || "left";

  var className = "krate-link-button krate-link-button-" + variant + " krate-link-button-" + size;
  if (props.class) className += " " + props.class;
  if (props.className) className += " " + props.className;

  return (
    <a class={className} href={href} id={props.id}>
      {icon !== "" && iconPosition === "left" ? <Icon name={icon} /> : null}
      <span>{children}</span>
      {icon !== "" && iconPosition === "right" ? <Icon name={icon} /> : null}
    </a>
  );
}
