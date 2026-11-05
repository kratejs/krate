import './context-menu.css';
import { createSignal, createEffect, onCleanup } from '@krate/runtime';

export interface ContextMenuProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/**
 * Context menu. Right-click, or press the ContextMenu key / Shift+F10 on the
 * focused trigger, to open it. Inside: ArrowUp/ArrowDown and Home/End move
 * between items, Enter or Space chooses, Escape closes and returns focus to the
 * trigger.
 */
export function ContextMenu(props: ContextMenuProps) {
  var [open, setOpen] = createSignal(false);
  var [menuPos, setMenuPos] = createSignal({ x: 0, y: 0 });
  var lastTrigger: HTMLElement | null = null;

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

  function enabledItems(root: Element): HTMLElement[] {
    return Array.prototype.slice.call(
      root.querySelectorAll("[data-krate-context-menu-item]:not([aria-disabled='true'])")
    ) as HTMLElement[];
  }

  function openAt(root: Element, trigger: HTMLElement, clientX: number, clientY: number) {
    var x = clientX;
    var y = clientY;
    var menuWidth = 200;
    var menuHeight = 200;
    if (x + menuWidth > window.innerWidth) x = window.innerWidth - menuWidth - 8;
    if (y + menuHeight > window.innerHeight) y = window.innerHeight - menuHeight - 8;
    if (x < 0) x = 8;
    if (y < 0) y = 8;
    lastTrigger = trigger;
    setMenuPos({ x: x, y: y });
    setOpen(true);
    // Keyboard users land on the first item once the menu is shown.
    requestAnimationFrame(function () {
      var items = enabledItems(root);
      if (items[0]) items[0].focus();
    });
  }

  function closeMenu(refocus: boolean) {
    setOpen(false);
    if (refocus && lastTrigger) lastTrigger.focus();
  }

  function handleContextMenu(e: MouseEvent) {
    var trigger = (e.target as HTMLElement).closest("[data-krate-context-menu-trigger]") as HTMLElement | null;
    if (trigger) {
      e.preventDefault();
      // A keyboard-invoked contextmenu event carries no pointer position.
      var x = e.clientX;
      var y = e.clientY;
      if (x === 0 && y === 0) {
        var r = trigger.getBoundingClientRect();
        x = r.left + 8;
        y = r.bottom - 8;
      }
      openAt(e.currentTarget as Element, trigger, x, y);
    }
  }

  function handleKeyDown(e: KeyboardEvent) {
    var root = e.currentTarget as HTMLElement;
    var target = e.target as HTMLElement;
    if (!open()) {
      var trigger = target.closest("[data-krate-context-menu-trigger]") as HTMLElement | null;
      if (trigger && (e.key === "ContextMenu" || (e.key === "F10" && e.shiftKey))) {
        e.preventDefault();
        var r = trigger.getBoundingClientRect();
        openAt(root, trigger, r.left + 8, r.bottom - 8);
      }
      return;
    }
    var items = enabledItems(root);
    var index = items.indexOf(document.activeElement as HTMLElement);
    if (e.key === "ArrowDown") {
      e.preventDefault();
      if (items.length) items[(index + 1) % items.length].focus();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      if (items.length) items[(index <= 0 ? items.length : index) - 1].focus();
    } else if (e.key === "Home") {
      e.preventDefault();
      if (items[0]) items[0].focus();
    } else if (e.key === "End") {
      e.preventDefault();
      if (items.length) items[items.length - 1].focus();
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      if (index >= 0) items[index].click();
    } else if (e.key === "Escape") {
      e.preventDefault();
      closeMenu(true);
    } else if (e.key === "Tab") {
      closeMenu(false);
    }
  }

  // Choosing an item closes the menu and hands focus back to the trigger.
  function handleClick(e: MouseEvent) {
    var item = (e.target as HTMLElement).closest("[data-krate-context-menu-item]");
    if (item && item.getAttribute("aria-disabled") !== "true") closeMenu(true);
  }

  return (
    <div
      class={"krate-context-menu" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}
      data-state={open() ? "open" : "closed"}
      onContextMenu={handleContextMenu}
      onKeyDown={handleKeyDown}
      onClick={handleClick}
      style={"--cm-x: " + menuPos().x + "px; --cm-y: " + menuPos().y + "px;"}
    >
      {props.children}
    </div>
  );
}

export interface ContextMenuTriggerProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/**
 * The area that opens the menu. It is focusable so keyboard users can reach it
 * and press the ContextMenu key or Shift+F10.
 */
/** @deprecated Use `ContextMenu.Trigger` instead. */
export function ContextMenuTrigger(props: ContextMenuTriggerProps) {
  return (
    <div
      class={"krate-context-menu-trigger" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}
      data-krate-context-menu-trigger="true"
      tabIndex={0}
      aria-haspopup="menu"
    >
      {props.children}
    </div>
  );
}

export interface ContextMenuContentProps {
  children?: any;
  /** Accessible name of the menu (default "Context menu"). */
  label?: string;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `ContextMenu.Content` instead. */
export function ContextMenuContent(props: ContextMenuContentProps) {
  return (
    <div
      class={"krate-context-menu-content" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}
      data-krate-context-menu-content="true"
      role="menu"
      aria-label={props.label || "Context menu"}
    >
      {props.children}
    </div>
  );
}

export interface ContextMenuItemProps {
  children?: any;
  disabled?: boolean;
  onSelect?: () => void;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `ContextMenu.Item` instead. */
export function ContextMenuItem(props: ContextMenuItemProps) {
  return (
    <div
      class={"krate-context-menu-item" + (props.disabled ? " krate-context-menu-item-disabled" : "") + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")}
      role="menuitem"
      tabIndex={-1}
      aria-disabled={props.disabled ? "true" : undefined}
      data-krate-context-menu-item="true"
      onClick={props.disabled ? undefined : props.onSelect}
    >
      {props.children}
    </div>
  );
}

export interface ContextMenuSeparatorProps {
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `ContextMenu.Separator` instead. */
export function ContextMenuSeparator(props: ContextMenuSeparatorProps) {
  return <div class={"krate-context-menu-separator" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} role="separator" />;
}

export interface ContextMenuLabelProps {
  children?: any;
  class?: string;
  /** Alias of `class`. */
  className?: string;
}

/** @deprecated Use `ContextMenu.Label` instead. */
export function ContextMenuLabel(props: ContextMenuLabelProps) {
  return <div class={"krate-context-menu-label" + (props.class ? " " + props.class : "") + (props.className ? " " + props.className : "")} role="presentation">{props.children}</div>;
}

export interface ContextMenu {
  Trigger: typeof ContextMenuTrigger;
  Content: typeof ContextMenuContent;
  Item: typeof ContextMenuItem;
  Separator: typeof ContextMenuSeparator;
  Label: typeof ContextMenuLabel;
}

ContextMenu.Trigger = ContextMenuTrigger;
ContextMenu.Content = ContextMenuContent;
ContextMenu.Item = ContextMenuItem;
ContextMenu.Separator = ContextMenuSeparator;
ContextMenu.Label = ContextMenuLabel;
