import './menu-bar.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';

const MenuBarCtx = createScope<string | null>();

export interface MenuBarProps {
  label?: string;
  children?: any;
}

/** Menu bar. Zero JS: one radio group whose selected option is the open menu. */
export function MenuBar(props: MenuBarProps) {
  const open = createCSSGroup<string>(null, { as: 'menubar', label: props.label });
  return (
    <MenuBarCtx.Provider value={open}>
      <div class="krate-menu-bar">{props.children}</div>
    </MenuBarCtx.Provider>
  );
}

export interface MenuBarTriggerProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `MenuBar.Trigger` instead. */
export function MenuBarTrigger(props: MenuBarTriggerProps) {
  const open = MenuBarCtx.use();
  return (
    <button class="krate-menu-bar-trigger" type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface MenuBarContentProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `MenuBar.Content` instead. */
export function MenuBarContent(props: MenuBarContentProps) {
  const open = MenuBarCtx.use();
  return (
    <div class="krate-menu-bar-content" {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface MenuBarItemProps {
  children?: any;
}

/** @deprecated Use `MenuBar.Item` instead. */
export function MenuBarItem(props: MenuBarItemProps) {
  const open = MenuBarCtx.use();
  return (
    <button class="krate-menu-bar-item" type="button" {...open.close()}>
      {props.children}
    </button>
  );
}

/** @deprecated Use `MenuBar.Separator` instead. */
export function MenuBarSeparator() {
  return <div class="krate-menu-bar-separator" role="separator" />;
}

export interface MenuBarLabelProps {
  children?: any;
}

/** @deprecated Use `MenuBar.Label` instead. */
export function MenuBarLabel(props: MenuBarLabelProps) {
  return <div class="krate-menu-bar-label">{props.children}</div>;
}

export interface MenuBar {
  Trigger: typeof MenuBarTrigger;
  Content: typeof MenuBarContent;
  Item: typeof MenuBarItem;
  Separator: typeof MenuBarSeparator;
  Label: typeof MenuBarLabel;
}

MenuBar.Trigger = MenuBarTrigger;
MenuBar.Content = MenuBarContent;
MenuBar.Item = MenuBarItem;
MenuBar.Separator = MenuBarSeparator;
MenuBar.Label = MenuBarLabel;
