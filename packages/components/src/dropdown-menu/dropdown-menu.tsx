import './dropdown-menu.css';
import { createSignal, createEffect, onCleanup } from '@krate/runtime';

export interface DropdownMenuProps {
  children?: any;
}

export function DropdownMenu(props: DropdownMenuProps) {
  var [open, setOpen] = createSignal(false);
  var rootRef: HTMLElement | null = null;

  function close() {
    setOpen(false);
  }

  function handleClick(e: MouseEvent) {
    var target = e.target as HTMLElement;
    var root = target.closest("[data-krate-dropdown-menu]") as HTMLElement;
    if (root) rootRef = root;

    var trigger = target.closest("[data-krate-dropdown-menu-trigger]");
    if (trigger) {
      setOpen(!open());
      return;
    }
    var item = target.closest("[data-krate-dropdown-menu-item]");
    if (item && open()) {
      setOpen(false);
    }
  }

  function handleClickOutside(e: MouseEvent) {
    var r = rootRef;
    if (r && !r.contains(e.target as Node)) {
      setOpen(false);
    }
  }

  createEffect(function () {
    // NOTE: The toggle (handleClick) is wired via the root's delegated onClick
    // slot. Registering it here at document level as well would cause every
    // trigger click to toggle TWICE (open then immediately close). Only the
    // click-outside close is registered at document level, and only while open.
    var outsideTimer: any = null;
    if (open()) {
      outsideTimer = setTimeout(function () {
        document.addEventListener("click", handleClickOutside);
      }, 0);
    }
    return function () {
      if (outsideTimer) clearTimeout(outsideTimer);
      document.removeEventListener("click", handleClickOutside);
    };
  });

  return (
    <div class="krate-dropdown-menu" data-krate-dropdown-menu="true" data-state={open() ? "open" : "closed"} onClick={handleClick}>
      {props.children}
    </div>
  );
}

export interface DropdownMenuTriggerProps {
  children?: any;
  asChild?: boolean;
}

export function DropdownMenuTrigger(props: DropdownMenuTriggerProps) {
  return (
    <button class="krate-dropdown-menu-trigger" type="button" aria-haspopup="menu" data-krate-dropdown-menu-trigger="true">
      {props.children}
    </button>
  );
}

export interface DropdownMenuContentProps {
  children?: any;
  align?: 'start' | 'center' | 'end';
}

export function DropdownMenuContent(props: DropdownMenuContentProps) {
  var children = props.children || "";
  var align = props.align || "start";
  var alignClass = align === "end" ? "krate-dropdown-menu-content-end" : align === "center" ? "krate-dropdown-menu-content-center" : "";

  return (
    <div class={"krate-dropdown-menu-content " + alignClass} data-krate-dropdown-menu-content="true">
      {children}
    </div>
  );
}

export interface DropdownMenuItemProps {
  children?: any;
  disabled?: boolean;
  onSelect?: () => void;
}

export function DropdownMenuItem(props: DropdownMenuItemProps) {
  return (
    <div class={"krate-dropdown-menu-item" + (props.disabled ? " krate-dropdown-menu-item-disabled" : "")} data-krate-dropdown-menu-item="true" onClick={props.onSelect}>
      {props.children}
    </div>
  );
}

export interface DropdownMenuSeparatorProps {
}

export function DropdownMenuSeparator(props: DropdownMenuSeparatorProps) {
  return <div class="krate-dropdown-menu-separator" />;
}

export interface DropdownMenuLabelProps {
  children?: any;
}

export function DropdownMenuLabel(props: DropdownMenuLabelProps) {
  return <div class="krate-dropdown-menu-label">{props.children}</div>;
}

export interface DropdownMenuGroupProps {
  children?: any;
}

export function DropdownMenuGroup(props: DropdownMenuGroupProps) {
  return <div class="krate-dropdown-menu-group">{props.children}</div>;
}

export interface DropdownMenuCheckboxItemProps {
  children?: any;
  checked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
}

export function DropdownMenuCheckboxItem(props: DropdownMenuCheckboxItemProps) {
  var [checked, setChecked] = createSignal(props.checked || false);

  function toggle() {
    var next = !checked();
    setChecked(next);
    if (props.onCheckedChange) {
      props.onCheckedChange(next);
    }
  }

  return (
    <div class="krate-dropdown-menu-item krate-dropdown-menu-checkbox-item" onClick={toggle}>
      <span class="krate-dropdown-menu-checkbox-indicator">
        {checked() ? (
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="20 6 9 17 4 12"/>
          </svg>
        ) : null}
      </span>
      {props.children}
    </div>
  );
}

export interface DropdownMenuRadioGroupProps {
  children?: any;
  value?: string;
  onValueChange?: (value: string) => void;
}

export function DropdownMenuRadioGroup(props: DropdownMenuRadioGroupProps) {
  return <div class="krate-dropdown-menu-radio-group">{props.children}</div>;
}

export interface DropdownMenuRadioItemProps {
  children?: any;
  value?: string;
}

export function DropdownMenuRadioItem(props: DropdownMenuRadioItemProps) {
  return (
    <div class="krate-dropdown-menu-item krate-dropdown-menu-radio-item">
      <span class="krate-dropdown-menu-radio-indicator"></span>
      {props.children}
    </div>
  );
}
