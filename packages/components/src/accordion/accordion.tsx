import './accordion.css';
import { createSignal, createEffect } from '@krate/runtime';

export interface AccordionProps {
  children?: any;
  type?: 'single' | 'multiple';
  defaultValue?: string[];
  value?: string[];
  onValueChange?: (value: string[]) => void;
  collapsible?: boolean;
}

export function Accordion(props: AccordionProps) {
  var type = props.type || "single";
  var collapsible = props.collapsible || false;
  var rootRef: HTMLElement | null = null;
  var [openItems, setOpenItems] = createSignal<string[]>(props.defaultValue || []);

  createEffect(function () {
    if (props.value !== undefined) {
      setOpenItems(props.value);
    }
  });

  createEffect(function () {
    var open = openItems();
    var root = rootRef;
    if (root) {
      syncDOM(root, open);
    }
  });

  function syncDOM(root: HTMLElement, open: string[]) {
    var items = root.querySelectorAll("[data-krate-accordion-item]");
    for (var i = 0; i < items.length; i++) {
      var el = items[i] as HTMLElement;
      var val = el.getAttribute("data-value");
      var isOpen = open.indexOf(val!) >= 0;
      el.setAttribute("data-state", isOpen ? "open" : "closed");
      var content = el.querySelector("[data-krate-accordion-content]") as HTMLElement;
      if (content) {
        content.setAttribute("data-state", isOpen ? "open" : "closed");
      }
    }
  }

  function handleClick(e: MouseEvent) {
    var trigger = (e.target as HTMLElement).closest("[data-krate-accordion-trigger]");
    if (trigger) {
      var item = trigger.closest("[data-krate-accordion-item]");
      if (item) {
        var value = item.getAttribute("data-value");
        var root = (e.target as HTMLElement).closest("[data-krate-accordion]") as HTMLElement;
        if (root) rootRef = root;
        var accType = root ? root.getAttribute("data-type") : "single";
        var accCollapsible = root ? root.getAttribute("data-collapsible") === "true" : false;
        var isOpen = item.getAttribute("data-state") === "open";
        if (value) {
          setOpenItems(function (prev) {
            var next: string[];
            if (accType === "single") {
              if (isOpen) {
                next = accCollapsible ? [] : [value!];
              } else {
                next = [value!];
              }
            } else {
              if (isOpen) {
                next = prev.filter(function (v) { return v !== value; });
              } else {
                next = prev.concat([value!]);
              }
            }
            if (root) {
              syncDOM(root, next);
            }
            return next;
          });
        }
      }
    }
  }

  return (
    <div class="krate-accordion" data-krate-accordion="true" data-type={type} data-collapsible={collapsible} onClick={handleClick}>
      {props.children}
    </div>
  );
}

export interface AccordionItemProps {
  children?: any;
  value?: string;
}

export function AccordionItem(props: AccordionItemProps) {
  var value = props.value || "";
  return (
    <div class="krate-accordion-item" data-value={value} data-state="closed" data-krate-accordion-item="true">
      {props.children}
    </div>
  );
}

export interface AccordionTriggerProps {
  children?: any;
  icon?: string;
}

export function AccordionTrigger(props: AccordionTriggerProps) {
  var children = props.children || "";
  return (
    <div class="krate-accordion-trigger" data-krate-accordion-trigger="true">
      <span class="krate-accordion-trigger-text">{children}</span>
      <span class="krate-accordion-trigger-icon">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="m6 9 6 6 6-6"/>
        </svg>
      </span>
    </div>
  );
}

export interface AccordionContentProps {
  children?: any;
}

export function AccordionContent(props: AccordionContentProps) {
  var children = props.children || "";
  return (
    <div class="krate-accordion-content" data-state="closed" data-krate-accordion-content="true">
      <div class="krate-accordion-content-inner">
        {children}
      </div>
    </div>
  );
}
