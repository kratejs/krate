import './navigation-menu.css';
import { createSignal, createEffect, onCleanup } from '@krate/runtime';

export interface NavigationMenuProps {
  children?: any;
}

export default function NavigationMenu(props: NavigationMenuProps) {
  return (
    <nav class="krate-navigation-menu" role="navigation">
      {props.children}
    </nav>
  );
}

export interface NavigationMenuListProps {
  children?: any;
}

export function NavigationMenuList(props: NavigationMenuListProps) {
  return (
    <ul class="krate-navigation-menu-list">
      {props.children}
    </ul>
  );
}

export interface NavigationMenuItemProps {
  children?: any;
}

export function NavigationMenuItem(props: NavigationMenuItemProps) {
  var [open, setOpen] = createSignal(false);
  var rootRef: HTMLElement | null = null;

  function registerCloseListener(root: HTMLElement) {
    if ((root as any).__krateNavMenuClose) return;
    (root as any).__krateNavMenuClose = true;
    root.addEventListener("krate:navigation-menu-close", function () {
      setOpen(false);
    });
  }

  function handleClick(e: MouseEvent) {
    var root = (e.target as HTMLElement).closest(".krate-navigation-menu-item") as HTMLElement;
    if (root) {
      rootRef = root;
      registerCloseListener(root);
    }

    var trigger = (e.target as HTMLElement).closest("[data-krate-navigation-menu-trigger]");
    if (trigger) {
      var next = !open();
      if (next && root) {
        var list = root.closest(".krate-navigation-menu-list") as HTMLElement;
        if (list) {
          var items = list.querySelectorAll(".krate-navigation-menu-item");
          for (var i = 0; i < items.length; i++) {
            var sibling = items[i] as HTMLElement;
            if (sibling !== root && sibling.getAttribute("data-state") === "open") {
              sibling.dispatchEvent(new CustomEvent("krate:navigation-menu-close"));
            }
          }
        }
      }
      setOpen(next);
      return;
    }
  }

  return (
    <li class="krate-navigation-menu-item" data-state={open() ? "open" : "closed"} onClick={handleClick}>
      {props.children}
    </li>
  );
}

export interface NavigationMenuTriggerProps {
  children?: any;
}

export function NavigationMenuTrigger(props: NavigationMenuTriggerProps) {
  return (
    <button class="krate-navigation-menu-trigger" type="button" aria-haspopup="menu" data-krate-navigation-menu-trigger="true">
      {props.children}
      <svg class="krate-navigation-menu-trigger-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="m6 9 6 6 6-6"/>
      </svg>
    </button>
  );
}

export interface NavigationMenuContentProps {
  children?: any;
}

export function NavigationMenuContent(props: NavigationMenuContentProps) {
  return (
    <div class="krate-navigation-menu-content" data-krate-navigation-menu-content="true">
      {props.children}
    </div>
  );
}

export interface NavigationMenuLinkProps {
  children?: any;
  href?: string;
}

export function NavigationMenuLink(props: NavigationMenuLinkProps) {
  return (
    <a class="krate-navigation-menu-link" href={props.href || "#"}>
      {props.children}
    </a>
  );
}

export interface NavigationMenuIndicatorProps {
}

export function NavigationMenuIndicator(props: NavigationMenuIndicatorProps) {
  return <div class="krate-navigation-menu-indicator" />;
}

export interface NavigationMenuViewportProps {
}

export function NavigationMenuViewport(props: NavigationMenuViewportProps) {
  return (
    <div class="krate-navigation-menu-viewport">
      {props.children}
    </div>
  );
}
