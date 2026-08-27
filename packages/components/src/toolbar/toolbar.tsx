import './toolbar.css';

export interface ToolbarProps {
  children?: any;
  orientation?: 'horizontal' | 'vertical';
}

export function Toolbar(props: ToolbarProps) {
  return (
    <div
      class="krate-toolbar"
      role="toolbar"
      data-orientation={props.orientation || "horizontal"}
    >
      {props.children}
    </div>
  );
}

export interface ToolbarSeparatorProps {
}

export function ToolbarSeparator(props: ToolbarSeparatorProps) {
  return <div class="krate-toolbar-separator" role="separator" />;
}

export interface ToolbarButtonProps {
  children?: any;
  disabled?: boolean;
  variant?: 'default' | 'outline';
}

export function ToolbarButton(props: ToolbarButtonProps) {
  var className = "krate-toolbar-button";
  if (props.variant === "outline") className += " krate-toolbar-button-outline";

  return (
    <button
      class={className}
      type="button"
      disabled={props.disabled}
    >
      {props.children}
    </button>
  );
}

export interface ToolbarLinkProps {
  children?: any;
  href?: string;
}

export function ToolbarLink(props: ToolbarLinkProps) {
  return (
    <a class="krate-toolbar-link" href={props.href || "#"}>
      {props.children}
    </a>
  );
}

export interface ToolbarGroupProps {
  children?: any;
}

export function ToolbarGroup(props: ToolbarGroupProps) {
  return <div class="krate-toolbar-group">{props.children}</div>;
}
