import './tabs.css';
import { createScope, createScopeFlags, createCSSChoice, type CSSScope } from '@krate/runtime';

const TabsCtx = createScope<string>();

export interface TabsProps {
  /** Literal initial tab value (which panel is shown first). */
  defaultValue?: string;
  children?: any;
}

/**
 * Tabs. Zero JS: the root declares a CSS scope and provides it via context; the
 * parts read it and their `{...scope.trigger/panel(value)}` spreads compile to
 * hidden radios + `:has()` CSS.
 *
 * ```tsx
 * <Tabs defaultValue="a">
 *   <Tabs.List>
 *     <Tabs.Trigger value="a">A</Tabs.Trigger>
 *   </Tabs.List>
 *   <Tabs.Content value="a">Panel</Tabs.Content>
 * </Tabs>
 * ```
 *
 * Option values must be literals so the compiler can enumerate them; for
 * data-driven options use the native `<Select>`.
 */
export function Tabs(props: TabsProps) {
  const tab = createCSSChoice<string>('', { as: 'tabs' });
  return (
    <TabsCtx.Provider value={tab}>
      <div class="krate-tabs">{props.children}</div>
    </TabsCtx.Provider>
  );
}

export interface TabsListProps {
  children?: any;
}

/** @deprecated Use `Tabs.List` instead. */
export function TabsList(props: TabsListProps) {
  return <div class="krate-tabs-list">{props.children}</div>;
}

export interface TabsTriggerProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `Tabs.Trigger` instead. */
export function TabsTrigger(props: TabsTriggerProps) {
  const tab = TabsCtx.use();
  return (
    <button class="krate-tabs-trigger" type="button" {...tab.trigger(props.value)}>
      {props.children}
    </button>
  );
}

export interface TabsContentProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `Tabs.Content` instead. */
export function TabsContent(props: TabsContentProps) {
  const tab = TabsCtx.use();
  return (
    <div class="krate-tabs-content" {...tab.panel(props.value)}>
      {props.children}
    </div>
  );
}

/** Namespaced compound parts (Radix-style). */
export interface Tabs {
  List: typeof TabsList;
  Trigger: typeof TabsTrigger;
  Content: typeof TabsContent;
}

Tabs.List = TabsList;
Tabs.Trigger = TabsTrigger;
Tabs.Content = TabsContent;
