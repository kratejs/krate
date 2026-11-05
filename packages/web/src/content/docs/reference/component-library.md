---
title: Component Library
description: The @krate/components UI kit.
order: 3
---

# Component Library

`@krate/components` is a shadcn/ui-style set of accessible UI components built
on the Krate runtime. To recolour them, override `--krate-primary` - see
[Theming Components](/docs/guides/theming/).

```tsx
import { Button, Card, CardGrid, Badge, Aside, Steps, Tabs, Tooltip } from '@krate/components';
```

## Layout & content

| Component | Description | Props |
|-----------|-------------|-------|
| `Card` | Card container with title and children | `title`, `titleAs`, `class`, `className`, `id` |
| `CardGrid` | Responsive grid of cards | `class`, `className`, `id` |
| `LinkCard` | Card that renders as a link | `title`, `href`, `icon`, `class`, `className`, `id` |
| `LinkButton` | Button that renders as a link | `href`, `variant`, `size`, `icon`, `iconPosition`, `class`, `className`, `id` |
| `Button` | `<button>` with variants, sizes, and `asChild` | `variant`, `size`, `type`, `disabled`, `asChild`, `class`, `onClick` |
| `Badge` | Small status/label pill (variants + sizes) | `variant`, `size`, `class`, `className`, `id` |
| `Code` | Inline/code block with highlighting | `lang`, `title`, `showCopy`, `highlight` |
| `Aside` | Callout / aside box | `type`, `title`, `icon`, `class`, `className` |
| `Alert` | Inline alert with title/description and variants | `variant`, `title`, `class`, `className`, `id` |
| `FileTree` | File tree display (`FileTree.Item`/`FileTree.Folder`) | `class`, `className` |
| `Steps` | Ordered steps (`Steps.Step`) with numbered indicators | `class`, `className` |
| `Tabs` | Tabbed panels (`Tabs.List`/`Tabs.Trigger`/`Tabs.Content`) | `defaultValue`, `class`, `className` |
| `Table` | Accessible table (`Table.Header`/`Table.Body`/`Table.Row`/`Table.Head`/`Table.Cell`/`Table.Caption`/`Table.Footer`) | `class` |
| `Breadcrumb` | Breadcrumb trail (`Breadcrumb.List`/`Breadcrumb.Item`/`Breadcrumb.Link`/`Breadcrumb.Page`/`Breadcrumb.Separator`) | `class` |
| `Pagination` | Prev/next + page links (pair with `paginationRange`) | `page`, `total`, `window`, `hrefPrefix`, `param`, `class`, `id`, `className` |
| `Skeleton` | CSS shimmer placeholder | `class` |

## Form controls

| Component | Description | Props |
|-----------|-------------|-------|
| `Checkbox` | Checkbox input | `name`, `value`, `defaultChecked`, `disabled`, `required`, `id`, `class`, `className`, `label` |
| `RadioGroup` | Grouped radio inputs | `defaultValue`, `label`, `class`, `className`, `id` |
| `Switch` | Toggle switch | `label`, `name`, `value`, `defaultChecked`, `disabled`, `required`, `id`, `class`, `className` |
| `Slider` | Range slider | `min`, `max`, `step`, `defaultValue`, `label`, `name`, `disabled`, `id`, `class`, `className` |
| `Select` / `NativeSelect` | Searchable select / native `<select>` | Select: `options`, `placeholder`, `name`, `label`, `defaultValue`, `disabled`, `id`, `class`, `className`; NativeSelect: `name`, `label`, `placeholder`, `options`, `defaultValue`, `disabled`, `id`, `required`, `class`, `className` |
| `Input` | Text input | `type`, `value`, `placeholder`, `name`, `id`, `disabled`, `required`, `readonly`, `autocomplete`, `class`, `onInput` |
| `Textarea` | Multi-line text input | `value`, `placeholder`, `name`, `id`, `rows`, `disabled`, `required`, `readonly`, `class`, `onInput` |
| `Label` | Form label | `htmlFor`, `disabled`, `required`, `class`, `className`, `id` |
| `Form` | Form container (`Form.Field`/`Form.Input`/`Form.Textarea`/`Form.Submit`) | `action`, `method`, `class`, `className`, `id` |
| `OTPField` | One-time-password input | `length`, `value`, `name`, `onValueChange`, `disabled`, `label`, `autoFocus`, `onComplete` |
| `PasswordToggleField` | Password input with visibility toggle | `placeholder`, `value`, `disabled`, `id`, `name`, `showToggle`, `required`, `autoComplete`, `class`, `className` |
| `Toggle` / `ToggleGroup` | Toggle button(s) | Toggle: `variant`, `size`, `label`, `id`, `class`, `className`; ToggleGroup: `defaultValue`, `label`, `class`, `className` |

## Overlays & navigation

| Component | Description | Props |
|-----------|-------------|-------|
| `Accordion` | Collapsible sections | `defaultValue`, `label`, `class`, `className` |
| `Collapsible` | Collapsible container | `defaultValue`, `label`, `class`, `className` |
| `Dialog` | Modal dialog | `label`, `defaultValue`, `defaultOpen`, `open`, `onOpenChange`, `id`, `class`, `className` |
| `AlertDialog` | Confirmation dialog | `label`, `defaultValue`, `defaultOpen`, `open`, `onOpenChange`, `id`, `class`, `className` |
| `Popover` | Floating popover | `label`, `defaultValue`, `defaultOpen`, `open`, `onOpenChange`, `id`, `class`, `className` |
| `Sheet` | Edge-anchored dialog/drawer (`Sheet.Trigger`/`Sheet.Content`/`Sheet.Close`/`Sheet.Title`/`Sheet.Description`) | `label`, `defaultValue`, `defaultOpen`, `open`, `onOpenChange`, `id`, `class`, `className` |
| `Command` | Filterable command list / palette | `items`, `placeholder`, `emptyText`, `label`, `class`, `id`, `onSelect` |
| `HoverCard` | Card revealed on hover | `id`, `class`, `className` |
| `DropdownMenu` | Menu triggered by a button | `label`, `defaultValue`, `defaultOpen`, `open`, `onOpenChange`, `id`, `class`, `className` |
| `ContextMenu` | Right-click menu | `class`, `className` |
| `MenuBar` | Horizontal menu bar | `label`, `class`, `className` |
| `NavigationMenu` | Navigation menu | `label`, `class`, `className` |
| `Tooltip` | Hover tooltip | `id`, `class`, `className` |
| `Toast` | Toast notifications | `variant` |

## Feedback & display

| Component | Description | Props |
|-----------|-------------|-------|
| `Progress` | Progress indicator | `value`, `max`, `label`, `class`, `className`, `id` |
| `Avatar` | Avatar image/fallback | `src`, `alt`, `fallback`, `size`, `class`, `className`, `id` |
| `Separator` | Horizontal/vertical divider | `orientation`, `decorative`, `style`, `class`, `className`, `id` |
| `AspectRatio` | Fixed aspect-ratio box | `ratio`, `class`, `className`, `id` |
| `ScrollArea` | Styled scroll container | `orientation`, `maxHeight`, `label`, `class`, `className`, `id` |
| `VisuallyHidden` | Accessible hidden content | `class`, `className`, `id` |
| `Toolbar` | Toolbar of controls | `label`, `class`, `className` |

## Zero-JS compound components

The interactive components use Krate's compiler-erased CSS signals and a
namespaced part API, so they ship **no page JavaScript bundle**. Compose them
with dotted parts and literal `value`s:

```tsx
<Accordion defaultValue="q1">
  <Accordion.Item>
    <Accordion.Trigger value="q1">Question</Accordion.Trigger>
    <Accordion.Content value="q1">Answer</Accordion.Content>
  </Accordion.Item>
</Accordion>
```

This applies to `Tabs`, `Accordion`/`AccordionMultiple`, `Collapsible`,
`RadioGroup`, `ToggleGroup`/`ToggleGroupMultiple`, `DropdownMenu`, `MenuBar`,
`NavigationMenu`, `Popover`, `Dialog`, `AlertDialog`, `Sheet`, and `Toast`. See
[CSS Scopes](/docs/core-concepts/css-signals/) for the model.

`Switch`, `Checkbox`, `Progress`, `NativeSelect`, `Tooltip`, and `HoverCard`
are built on native elements or pure CSS. `Select`, `ContextMenu`, `Command`,
`OTPField`, `PasswordToggleField`, and `Pagination` use runtime signals and
hydrate on the page.

Every compound component is used **namespaced** - `Breadcrumb.List`,
`Table.Row`, `Steps.Step`, `Form.Field`, `FileTree.Folder`, `Tabs.Trigger`,
`Dialog.Content`, `Toolbar.Button`, and so on. The flat exports
(`BreadcrumbList`, `TableRow`, ...) still work as deprecated aliases.

An instance's `value`s must be literals. Each instance on a page gets its own
option set, so two `Dialog`s can use different content `value`s.

For data-driven option sets (options known only at runtime), use the native
`<Select options={...}/>` component rather than a scope component.

## Accessibility and keyboard

Overlay and disclosure components work with a pointer through CSS alone. Pages
that use one also inline a small (about 3 KB) keyboard and ARIA enhancer, so the
same components are fully keyboard operable:

| Component | Keyboard | ARIA |
|-----------|----------|------|
| `Dialog`, `AlertDialog`, `Sheet` | Enter/Space on the trigger opens and moves focus into the dialog; Tab and Shift+Tab stay inside; Escape or `Close` closes and returns focus to the trigger | trigger: `role=button`, `aria-haspopup=dialog`, `aria-expanded`, `aria-controls`; panel: `role=dialog` (`alertdialog` for `AlertDialog`), `aria-modal`, `aria-labelledby` from `*.Title`, `aria-describedby` from `*.Description` (or `label`) |
| `Popover` | Enter/Space opens and moves focus in; Escape closes and restores focus | as above, non-modal |
| `DropdownMenu` | Enter/Space/ArrowDown opens and focuses the first item; Up/Down/Home/End move; Escape or activating an item closes and restores focus | trigger `aria-haspopup=menu`; panel `role=menu`; items `role=menuitem` |
| `MenuBar` | as `DropdownMenu`, plus Left/Right between menus | as `DropdownMenu` |
| `NavigationMenu`, `Accordion`, `Collapsible` | Enter/Space toggles and keeps focus | trigger `role=button`, `aria-expanded`, `aria-controls` |
| `Tabs` | Roving tabindex; Left/Right/Home/End select | `role=tablist` > `role=tab` with `aria-selected` |
| `Switch` | Space toggles (a native checkbox) | `role=switch` on the input, named by `label` |

The hidden controller inputs are `aria-hidden` and out of the tab order, so
nothing unnamed is announced. With JavaScript disabled, mouse and touch still
work; keyboard operation needs the enhancer.

`OTPField` names each digit ("Verification code digit 1 of 6"; set `label` to
change the prefix) and `Command` names its input and list (`label`).

### Contrast

The default `--krate-primary` is `#c2410c`, which keeps white text on every
primary `Button`, `Badge`, `LinkButton`, and active `Pagination` page at 4.5:1 or
better (AA). `--krate-primary-text` is the same hue as readable *text* on the
page background (lighter in dark mode), and `--krate-danger-text`,
`--krate-success-text`, and `--krate-warning-text` do the same for status text.
See [Theming Components](/docs/guides/theming/).

### Right-to-left

Component CSS uses logical properties (`margin-inline-start`,
`inset-inline-end`, `border-inline-start`, `text-align: start`), so setting
`dir="rtl"` on `<html>` mirrors `Steps`, `LinkCard`, `Label`,
`PasswordToggleField`, `Table`, `Switch`, `Tooltip`, `HoverCard`, and `Popover`.

## Controlling overlays

`Dialog`, `AlertDialog`, `Sheet`, `Popover`, and `DropdownMenu` accept:

| Prop | Meaning |
|------|---------|
| `defaultOpen` | Open the first content on load (a literal, no JS) |
| `defaultValue` | Open the content with this literal `value` on load (no JS) |
| `open` | Controlled state; `true` opens the first content, `false` closes it |
| `onOpenChange` | Called with `true`/`false` whenever the overlay opens or closes |
| `id` | Element id, a handle for the functions below |

Passing `open` or `onOpenChange` hydrates a small bridge for that overlay only;
overlays that use neither stay free of page JS.

```tsx
const [open, setOpen] = createSignal(false);

<Dialog open={open()} onOpenChange={setOpen}>
  <Dialog.Content value="c">
    <Dialog.Title>Edit profile</Dialog.Title>
    <Dialog.Close>Close</Dialog.Close>
  </Dialog.Content>
</Dialog>
```

From any client code, `openOverlay('#id', value?)`, `closeOverlay('#id')`,
`setOverlayOpen('#id', open)`, `isOverlayOpen('#id')`, and
`onOverlayChange('#id', cb)` drive an overlay that has an `id` (or any element
inside it). They return `false` when the target is not an overlay.

### Toasts

`<Toast>` renders a toast in the page. For notifications from event handlers,
call `toast()`; it appends to an `aria-live="polite"` region at the bottom
corner and uses the same styles:

```tsx
import { toast } from '@krate/components';

toast({ title: 'Saved', description: 'Your changes were saved.', variant: 'success' });
```

Options: `title`, `description`, `variant` (`default`, `success`, `destructive`),
and `duration` in milliseconds (default 5000; `0` keeps it until closed).
`toast()` returns `{ id, dismiss }` and is client-only; calling it during
server rendering throws.

## Not yet included

These larger components are planned and not part of the library yet: Combobox,
DatePicker/Calendar, DataTable, Carousel.
