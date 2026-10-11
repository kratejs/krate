import './toolbar.css';


export interface ToolbarProps {
  label?: string;
  children?: any;
}

/** Toolbar container. Presentational: zero JS. */
export function Toolbar(props: ToolbarProps) {
  return (
    <div class="krate-toolbar" role="toolbar" aria-label={props.label}>
      {props.children}
    </div>
  );
}

export interface ToolbarButtonProps {
  children?: any;
}

/** @deprecated Use `Toolbar.Button` instead. */
export function ToolbarButton(props: ToolbarButtonProps) {
  return (
    <button class="krate-toolbar-button" type="button">
      {props.children}
    </button>
  );
}

export interface ToolbarLinkProps {
  href?: string;
  children?: any;
}

/** @deprecated Use `Toolbar.Link` instead. */
export function ToolbarLink(props: ToolbarLinkProps) {
  return (
    <a class="krate-toolbar-link" href={props.href}>
      {props.children}
    </a>
  );
}

/** @deprecated Use `Toolbar.Separator` instead. */
export function ToolbarSeparator() {
  return <div class="krate-toolbar-separator" role="separator" />;
}

export interface ToolbarGroupProps {
  children?: any;
}

/** @deprecated Use `Toolbar.Group` instead. */
export function ToolbarGroup(props: ToolbarGroupProps) {
  return (
    <div class="krate-toolbar-group" role="group">
      {props.children}
    </div>
  );
}

export interface Toolbar {
  Button: typeof ToolbarButton;
  Link: typeof ToolbarLink;
  Separator: typeof ToolbarSeparator;
  Group: typeof ToolbarGroup;
}

Toolbar.Button = ToolbarButton;
Toolbar.Link = ToolbarLink;
Toolbar.Separator = ToolbarSeparator;
Toolbar.Group = ToolbarGroup;
