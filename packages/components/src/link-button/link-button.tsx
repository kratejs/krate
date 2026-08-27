import './link-button.css';

export interface LinkButtonProps {
  children?: any;
  href?: string;
  variant?: 'primary' | 'secondary' | 'ghost' | 'destructive';
  size?: 'sm' | 'md' | 'lg';
  icon?: string;
  iconPosition?: 'left' | 'right';
}

export function LinkButton(props: LinkButtonProps) {
  var children = props.children || "";
  var href = props.href || "#";
  var variant = props.variant || "primary";
  var size = props.size || "md";
  var icon = props.icon || "";
  var iconPosition = props.iconPosition || "left";

  var className = "krate-link-button krate-link-button-" + variant + " krate-link-button-" + size;

  return (
    <a class={className} href={href}>
      {icon !== "" && iconPosition === "left" ? <Icon name={icon} /> : null}
      <span>{children}</span>
      {icon !== "" && iconPosition === "right" ? <Icon name={icon} /> : null}
    </a>
  );
}
