import './dropdown-menu.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';

const DropdownMenuCtx = createScope<string | null>();

export interface DropdownMenuProps {
  label?: string;
  children?: any;
}

/**
 * Dropdown menu. Zero JS via a context-provided CSS scope.
 *
 * ```tsx
 * <DropdownMenu>
 *   <DropdownMenu.Trigger value="menu">Open</DropdownMenu.Trigger>
 *   <DropdownMenu.Content value="menu">
 *     <DropdownMenu.Item>Profile</DropdownMenu.Item>
 *   </DropdownMenu.Content>
 * </DropdownMenu>
 * ```
 */
export function DropdownMenu(props: DropdownMenuProps) {
  const open = createCSSGroup<string>(null, { as: 'menu', label: props.label });
  return (
    <DropdownMenuCtx.Provider value={open}>
      <div class="krate-dropdown-menu">{props.children}</div>
    </DropdownMenuCtx.Provider>
  );
}

export interface DropdownMenuTriggerProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `DropdownMenu.Trigger` instead. */
export function DropdownMenuTrigger(props: DropdownMenuTriggerProps) {
  const open = DropdownMenuCtx.use();
  return (
    <button class="krate-dropdown-menu-trigger" type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface DropdownMenuContentProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `DropdownMenu.Content` instead. */
export function DropdownMenuContent(props: DropdownMenuContentProps) {
  const open = DropdownMenuCtx.use();
  return (
    <div class="krate-dropdown-menu-content" {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface DropdownMenuItemProps {
  children?: any;
}

/** @deprecated Use `DropdownMenu.Item` instead. */
export function DropdownMenuItem(props: DropdownMenuItemProps) {
  const open = DropdownMenuCtx.use();
  return (
    <button class="krate-dropdown-menu-item" type="button" {...open.close()}>
      {props.children}
    </button>
  );
}

/** @deprecated Use `DropdownMenu.Separator` instead. */
export function DropdownMenuSeparator() {
  return <div class="krate-dropdown-menu-separator" role="separator" />;
}

export interface DropdownMenuLabelProps {
  children?: any;
}

/** @deprecated Use `DropdownMenu.Label` instead. */
export function DropdownMenuLabel(props: DropdownMenuLabelProps) {
  return <div class="krate-dropdown-menu-label">{props.children}</div>;
}

export interface DropdownMenu {
  Trigger: typeof DropdownMenuTrigger;
  Content: typeof DropdownMenuContent;
  Item: typeof DropdownMenuItem;
  Separator: typeof DropdownMenuSeparator;
  Label: typeof DropdownMenuLabel;
}

DropdownMenu.Trigger = DropdownMenuTrigger;
DropdownMenu.Content = DropdownMenuContent;
DropdownMenu.Item = DropdownMenuItem;
DropdownMenu.Separator = DropdownMenuSeparator;
DropdownMenu.Label = DropdownMenuLabel;
