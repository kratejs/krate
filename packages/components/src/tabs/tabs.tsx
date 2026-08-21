import './tabs.css';
import { createSignal, createEffect, onMount } from '@krate/runtime';

export interface TabsProps {
  children?: any;
  defaultValue?: string;
  value?: string;
  onValueChange?: (value: string) => void;
  orientation?: 'horizontal' | 'vertical';
}

export default function Tabs(props: TabsProps) {
  var orientation = props.orientation || "horizontal";
  var [active, setActive] = createSignal(props.defaultValue || "");

  createEffect(function () {
    if (props.value !== undefined) {
      setActive(props.value);
    }
  });

  function handleChange(val: string) {
    setActive(val);
    if (props.onValueChange) {
      props.onValueChange(val);
    }
  }

  function handleClick(e: MouseEvent) {
    var trigger = (e.target as HTMLElement).closest("[data-krate-tabs-trigger]");
    if (trigger) {
      var value = trigger.getAttribute("data-value");
      if (value) {
        handleChange(value);
      }
    }
  }

  createEffect(function () {
    var activeValue = active();
    var tabs = document.querySelector(".krate-tabs");
    if (!tabs) return;
    var triggers = tabs.querySelectorAll("[data-krate-tabs-trigger]");
    for (var i = 0; i < triggers.length; i++) {
      var el = triggers[i] as HTMLElement;
      var val = el.getAttribute("data-value");
      el.setAttribute("data-state", val === activeValue ? "active" : "inactive");
    }
    var contents = tabs.querySelectorAll("[data-krate-tabs-content]");
    for (var i = 0; i < contents.length; i++) {
      var el = contents[i] as HTMLElement;
      var val = el.getAttribute("data-value");
      el.setAttribute("data-state", val === activeValue ? "active" : "inactive");
    }
  });

  return (
    <div class="krate-tabs" data-orientation={orientation} data-state="active" data-active-tab={active()} onClick={handleClick}>
      {props.children}
    </div>
  );
}

export interface TabsListProps {
  children?: any;
}

export function TabsList(props: TabsListProps) {
  return (
    <div class="krate-tabs-list" role="tablist">
      {props.children}
    </div>
  );
}

export interface TabsTriggerProps {
  children?: any;
  value?: string;
}

export function TabsTrigger(props: TabsTriggerProps) {
  var value = props.value || "";
  return (
    <button
      class="krate-tabs-trigger"
      role="tab"
      data-value={value}
      data-krate-tabs-trigger="true"
    >
      {props.children}
    </button>
  );
}

export interface TabsContentProps {
  children?: any;
  value?: string;
}

export function TabsContent(props: TabsContentProps) {
  var value = props.value || "";
  return (
    <div
      class="krate-tabs-content"
      role="tabpanel"
      data-value={value}
      data-krate-tabs-content="true"
    >
      {props.children}
    </div>
  );
}
