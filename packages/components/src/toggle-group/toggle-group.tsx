import './toggle-group.css';
import { createSignal, createEffect } from '@krate/runtime';

interface ToggleGroupBaseProps {
  children?: any;
  type?: 'single' | 'multiple';
  orientation?: 'horizontal' | 'vertical';
  size?: 'sm' | 'md' | 'lg';
}

export interface ToggleGroupSingleProps extends ToggleGroupBaseProps {
  type?: 'single';
  value?: string;
  defaultValue?: string;
  onValueChange?: (value: string) => void;
}

export interface ToggleGroupMultipleProps extends ToggleGroupBaseProps {
  type?: 'multiple';
  value?: string[];
  defaultValue?: string[];
  onValueChange?: (value: string[]) => void;
}

export type ToggleGroupProps = ToggleGroupSingleProps | ToggleGroupMultipleProps;

export function ToggleGroup(props: ToggleGroupProps) {
  var type = props.type || "single";
  var [selected, setSelected] = createSignal<string | string[]>(
    type === "single"
      ? ((props.defaultValue as string) || "")
      : ((props.defaultValue as string[]) || [])
  );
  var rootRef: HTMLElement | null = null;

  createEffect(function () {
    if (props.value !== undefined) {
      setSelected(props.value);
    }
  });

  function syncItems(root: HTMLElement, selectedValue: string | string[]) {
    var groupType = root.getAttribute("data-type");
    var items = root.querySelectorAll("[data-krate-toggle-group-item]");
    for (var i = 0; i < items.length; i++) {
      var el = items[i] as HTMLElement;
      var val = el.getAttribute("data-value");
      var isActive = false;
      if (groupType === "single") {
        isActive = selectedValue === val;
      } else if (Array.isArray(selectedValue)) {
        isActive = selectedValue.indexOf(val!) >= 0;
      }
      el.setAttribute("data-state", isActive ? "on" : "off");
    }
  }

  function handleClick(e: MouseEvent) {
    var target = e.target as HTMLElement;
    var root = target.closest("[data-krate-toggle-group]") as HTMLElement;
    if (root) rootRef = root;

    var item = target.closest("[data-krate-toggle-group-item]");
    if (item) {
      var value = item.getAttribute("data-value");
      if (value) {
        var groupType = root ? root.getAttribute("data-type") : "single";
        if (groupType === "single") {
          var current = selected() as string;
          var next = current === value ? "" : value;
          setSelected(next);
          if (props.onValueChange) (props.onValueChange as (value: string) => void)(next);
          if (root) syncItems(root, next);
        } else {
          var currentArr = selected() as string[];
          var idx = currentArr.indexOf(value);
          var nextArr: string[];
          if (idx >= 0) {
            nextArr = currentArr.filter(function (v) { return v !== value; });
          } else {
            nextArr = currentArr.concat([value]);
          }
          setSelected(nextArr);
          if (props.onValueChange) (props.onValueChange as (value: string[]) => void)(nextArr);
          if (root) syncItems(root, nextArr);
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
      class={"krate-toggle-group" + (props.size ? " krate-toggle-group-" + props.size : "")}
      data-krate-toggle-group="true"
      role="group"
      data-orientation={props.orientation || "horizontal"}
      data-type={type}
      onClick={handleClick}
    >
      {props.children}
    </div>
  );
}

export interface ToggleGroupItemProps {
  value?: string;
  disabled?: boolean;
  children?: any;
}

export function ToggleGroupItem(props: ToggleGroupItemProps) {
  return (
    <button
      class="krate-toggle-group-item"
      type="button"
      role="checkbox"
      data-value={props.value || ""}
      data-krate-toggle-group-item="true"
      disabled={props.disabled}
    >
      {props.children}
    </button>
  );
}
