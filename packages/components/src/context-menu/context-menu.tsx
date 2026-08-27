import './context-menu.css';
import { createSignal, createEffect, onCleanup } from '@krate/runtime';

export interface ContextMenuProps {
  children?: any;
}

export function ContextMenu(props: ContextMenuProps) {
  var [open, setOpen] = createSignal(false);
  var [menuPos, setMenuPos] = createSignal({ x: 0, y: 0 });

  function handleClickOutside() {
    setOpen(false);
  }

  createEffect(function () {
    var handler = handleClickOutside;
    var timer: any = null;
    if (open()) {
      timer = setTimeout(function () {
        document.addEventListener("click", handler);
      }, 0);
    }
    return function () {
      if (timer) clearTimeout(timer);
      document.removeEventListener("click", handler);
    };
  });

  function handleContextMenu(e: MouseEvent) {
    var trigger = (e.target as HTMLElement).closest("[data-krate-context-menu-trigger]");
    if (trigger) {
      e.preventDefault();
      var x = e.clientX;
      var y = e.clientY;
      var menuWidth = 200;
      var menuHeight = 200;
      if (x + menuWidth > window.innerWidth) x = window.innerWidth - menuWidth - 8;
      if (y + menuHeight > window.innerHeight) y = window.innerHeight - menuHeight - 8;
      if (x < 0) x = 8;
      if (y < 0) y = 8;
      setMenuPos({ x: x, y: y });
      setOpen(true);
    }
  }

  return (
    <div class="krate-context-menu" data-state={open() ? "open" : "closed"} onContextMenu={handleContextMenu} style={"--cm-x: " + menuPos().x + "px; --cm-y: " + menuPos().y + "px;"}>
      {props.children}
    </div>
  );
}

export interface ContextMenuTriggerProps {
  children?: any;
}

export function ContextMenuTrigger(props: ContextMenuTriggerProps) {
  return (
    <div class="krate-context-menu-trigger" data-krate-context-menu-trigger="true">
      {props.children}
    </div>
  );
}

export interface ContextMenuContentProps {
  children?: any;
}

export function ContextMenuContent(props: ContextMenuContentProps) {
  return (
    <div class="krate-context-menu-content" data-krate-context-menu-content="true">
      {props.children}
    </div>
  );
}

export interface ContextMenuItemProps {
  children?: any;
  disabled?: boolean;
  onSelect?: () => void;
}

export function ContextMenuItem(props: ContextMenuItemProps) {
  return (
    <div class={"krate-context-menu-item" + (props.disabled ? " krate-context-menu-item-disabled" : "")} data-krate-context-menu-item="true" onClick={props.onSelect}>
      {props.children}
    </div>
  );
}

export interface ContextMenuSeparatorProps {
}

export function ContextMenuSeparator(props: ContextMenuSeparatorProps) {
  return <div class="krate-context-menu-separator" />;
}

export interface ContextMenuLabelProps {
  children?: any;
}

export function ContextMenuLabel(props: ContextMenuLabelProps) {
  return <div class="krate-context-menu-label">{props.children}</div>;
}
