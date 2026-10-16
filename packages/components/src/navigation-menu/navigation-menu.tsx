import './navigation-menu.css';
import { createScope, createScopeFlags, createCSSGroup, type CSSScope } from '@krate/runtime';

const NavigationMenuCtx = createScope<string | null>();

export interface NavigationMenuProps {
  label?: string;
  children?: any;
}

/** Navigation menu. Zero JS: one radio group whose selected option is open. */
export function NavigationMenu(props: NavigationMenuProps) {
  const open = createCSSGroup<string>(null, { as: 'menu', label: props.label });
  return (
    <NavigationMenuCtx.Provider value={open}>
      <div class="krate-navigation-menu">{props.children}</div>
    </NavigationMenuCtx.Provider>
  );
}

export interface NavigationMenuTriggerProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `NavigationMenu.Trigger` instead. */
export function NavigationMenuTrigger(props: NavigationMenuTriggerProps) {
  const open = NavigationMenuCtx.use();
  return (
    <button class="krate-navigation-menu-trigger" type="button" {...open.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface NavigationMenuContentProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `NavigationMenu.Content` instead. */
export function NavigationMenuContent(props: NavigationMenuContentProps) {
  const open = NavigationMenuCtx.use();
  return (
    <div class="krate-navigation-menu-content" {...open.panel(props.value)}>
      {props.children}
    </div>
  );
}

export interface NavigationMenuLinkProps {
  href?: string;
  children?: any;
}

/** @deprecated Use `NavigationMenu.Link` instead. */
export function NavigationMenuLink(props: NavigationMenuLinkProps) {
  return (
    <a class="krate-navigation-menu-link" href={props.href}>
      {props.children}
    </a>
  );
}

export interface NavigationMenu {
  Trigger: typeof NavigationMenuTrigger;
  Content: typeof NavigationMenuContent;
  Link: typeof NavigationMenuLink;
}

NavigationMenu.Trigger = NavigationMenuTrigger;
NavigationMenu.Content = NavigationMenuContent;
NavigationMenu.Link = NavigationMenuLink;
