import './menu-bar.css';
import { createSignal } from '@krate/runtime';

export interface MenuBarProps {
  children?: any;
}

export default function MenuBar(props: MenuBarProps) {
  return (
    <div class="krate-menu-bar" role="menubar">
      {props.children}
    </div>
  );
}

export interface MenuBarMenuProps {
  children?: any;
}

export function MenuBarMenu(props: MenuBarMenuProps) {
  var [open, setOpen] = createSignal(false);
  var rootRef: HTMLElement | null = null;

  function registerCloseListener(root: HTMLElement) {
    if ((root as any).__krateMenuBarClose) return;
    (root as any).__krateMenuBarClose = true;
    root.addEventListener("krate:menu-bar-close", function () {
      setOpen(false);
    });
  }

  function handleClick(e: MouseEvent) {
    var root = (e.target as HTMLElement).closest(".krate-menu-bar-menu") as HTMLElement;
    if (root) {
      rootRef = root;
      registerCloseListener(root);
    }

    var trigger = (e.target as HTMLElement).closest("[data-krate-menu-bar-trigger]");
    if (trigger) {
      var next = !open();
      if (next && root) {
        var bar = root.closest(".krate-menu-bar") as HTMLElement;
        if (bar) {
          var menus = bar.querySelectorAll(".krate-menu-bar-menu");
          for (var i = 0; i < menus.length; i++) {
            var sibling = menus[i] as HTMLElement;
            if (sibling !== root && sibling.getAttribute("data-state") === "open") {
              sibling.dispatchEvent(new CustomEvent("krate:menu-bar-close"));
            }
          }
        }
      }
      setOpen(next);
      return;
    }
    var item = (e.target as HTMLElement).closest("[data-krate-menu-bar-item]");
    if (item && open()) {
      setOpen(false);
    }
  }

  return (
    <div class="krate-menu-bar-menu" data-state={open() ? "open" : "closed"} onClick={handleClick}>
      {props.children}
    </div>
  );
}

export interface MenuBarTriggerProps {
  children?: any;
}

export function MenuBarTrigger(props: MenuBarTriggerProps) {
  return (
    <button class="krate-menu-bar-trigger" type="button" role="menuitem" data-krate-menu-bar-trigger="true">
      {props.children}
    </button>
  );
}

export interface MenuBarContentProps {
  children?: any;
}

export function MenuBarContent(props: MenuBarContentProps) {
  return (
    <div class="krate-menu-bar-content" role="menu" data-krate-menu-bar-content="true">
      {props.children}
    </div>
  );
}

export interface MenuBarItemProps {
  children?: any;
  disabled?: boolean;
}

export function MenuBarItem(props: MenuBarItemProps) {
  return (
    <div class={"krate-menu-bar-item" + (props.disabled ? " krate-menu-bar-item-disabled" : "")} role="menuitem" data-krate-menu-bar-item="true">
      {props.children}
    </div>
  );
}

export interface MenuBarSeparatorProps {
}

export function MenuBarSeparator(props: MenuBarSeparatorProps) {
  return <div class="krate-menu-bar-separator" role="separator" />;
}

export interface MenuBarLabelProps {
  children?: any;
}

export function MenuBarLabel(props: MenuBarLabelProps) {
  return <div class="krate-menu-bar-label">{props.children}</div>;
}
