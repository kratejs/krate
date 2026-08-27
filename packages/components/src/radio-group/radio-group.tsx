import './radio-group.css';
import { createSignal, createEffect } from '@krate/runtime';

export interface RadioGroupProps {
  children?: any;
  value?: string;
  defaultValue?: string;
  onValueChange?: (value: string) => void;
  name?: string;
  orientation?: 'horizontal' | 'vertical';
}

export function RadioGroup(props: RadioGroupProps) {
  var [selected, setSelected] = createSignal(props.defaultValue || "");
  var rootRef: HTMLElement | null = null;

  createEffect(function () {
    if (props.value !== undefined) {
      setSelected(props.value);
    }
  });

  function handleChange(val: string) {
    setSelected(val);
    if (props.onValueChange) {
      props.onValueChange(val);
    }
  }

  function syncItems(root: HTMLElement, selectedValue: string) {
    var items = root.querySelectorAll("[data-krate-radio-group-item]");
    for (var i = 0; i < items.length; i++) {
      var el = items[i] as HTMLElement;
      var val = el.getAttribute("data-value");
      el.setAttribute("data-state", val === selectedValue ? "checked" : "unchecked");
    }
  }

  function handleClick(e: MouseEvent) {
    var target = e.target as HTMLElement;
    var root = target.closest("[data-krate-radio-group]") as HTMLElement;
    if (root) rootRef = root;

    var item = target.closest("[data-krate-radio-group-item]");
    if (item) {
      var value = item.getAttribute("data-value");
      if (value) {
        handleChange(value);
        if (root) {
          syncItems(root, value);
        }
      }
    }
  }

  createEffect(function () {
    var selectedValue = selected();
    var root = rootRef;
    if (root) {
      syncItems(root, selectedValue);
    }
  });

  return (
    <div
      class="krate-radio-group"
      data-krate-radio-group="true"
      role="radiogroup"
      data-orientation={props.orientation || "vertical"}
      onClick={handleClick}
    >
      {props.children}
    </div>
  );
}

export interface RadioGroupItemProps {
  value?: string;
  disabled?: boolean;
  children?: any;
}

export function RadioGroupItem(props: RadioGroupItemProps) {
  var value = props.value || "";

  return (
    <button
      class={"krate-radio-group-item" + (props.disabled ? " krate-radio-group-item-disabled" : "")}
      role="radio"
      type="button"
      data-value={value}
      data-krate-radio-group-item="true"
      disabled={props.disabled}
    >
      <span class="krate-radio-group-item-indicator">
        <span class="krate-radio-group-item-indicator-dot" />
      </span>
    </button>
  );
}

export interface RadioGroupLabelProps {
  children?: any;
}

export function RadioGroupLabel(props: RadioGroupLabelProps) {
  return (
    <span class="krate-radio-group-label">{props.children}</span>
  );
}
