import './dropdown-menu.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';
import { OverlayBridge } from '../overlay/overlay-bridge';

const DropdownMenuCtx = createScope<string | null>();

export interface DropdownMenuProps {
  label?: string;
  /** Value of the content shown initially (a literal, like Tabs `defaultValue`). */
  defaultValue?: string;
  /** Open the first content initially. */
  defaultOpen?: boolean;
  /** Controlled state: true opens the first content, false closes it. Hydrates a small bridge. */
  open?: boolean;
  /** Called with the new state when the overlay opens or closes. Hydrates a small bridge. */
  onOpenChange?: (open: boolean) => void;
  /** Element id: a handle for `openOverlay('#id')` / `closeOverlay('#id')`. */
  id?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
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
      <div class={"krate-dropdown-menu" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} id={props.id}>
        {props.open !== undefined || props.onOpenChange ? <OverlayBridge open={props.open} onOpenChange={props.onOpenChange} /> : null}
        {props.children}
      </div>
    </DropdownMenuCtx.Provider>
  );
}

export interface DropdownMenuTriggerProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `DropdownMenu.Trigger` instead. */
export function DropdownMenuTrigger(props: DropdownMenuTriggerProps) {
  const open = DropdownMenuCtx.use();
  return (
    <button class={"krate-dropdown-menu-trigger" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface DropdownMenuContentProps {
  value?: string;
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `DropdownMenu.Content` instead. */
export function DropdownMenuContent(props: DropdownMenuContentProps) {
  const open = DropdownMenuCtx.use();
  return (
    <div class={"krate-dropdown-menu-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface DropdownMenuItemProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `DropdownMenu.Item` instead. */
export function DropdownMenuItem(props: DropdownMenuItemProps) {
  const open = DropdownMenuCtx.use();
  return (
    <button class={"krate-dropdown-menu-item" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} type="button" {...open.close()}>
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
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `DropdownMenu.Label` instead. */
export function DropdownMenuLabel(props: DropdownMenuLabelProps) {
  return <div class={"krate-dropdown-menu-label" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}>{props.children}</div>;
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
