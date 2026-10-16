---
title: Component Library
description: The @krate/components UI kit.
order: 3
---

# Component Library

`@krate/components` is a shadcn/ui-style set of accessible UI components built
on the Krate runtime. To recolour them, override `--krate-primary` — see
[Theming Components](/docs/guides/theming/).

```tsx
import { Button, Card, CardGrid, Badge, Aside, Steps, Tabs, Tooltip } from '@krate/components';
```

## Layout & content

| Component | Description |
|-----------|-------------|
| `Card` | Card container with title and children |
| `CardGrid` | Responsive grid of cards |
| `LinkCard` | Card that renders as a link |
| `LinkButton` | Button that renders as a link |
| `Button` | `<button>` with variants, sizes, and `asChild` |
| `Badge` | Small status/label pill (variants + sizes) |
| `Code` | Inline/code block with highlighting |
| `Aside` | Callout / aside box |
| `Alert` | Inline alert with title/description and variants |
| `FileTree` | File tree display (`FileTree.Item`/`FileTree.Folder`) |
| `Steps` | Ordered steps (`Steps.Step`) with numbered indicators |
| `Tabs` | Tabbed panels (`Tabs.List`/`Tabs.Trigger`/`Tabs.Content`) |
| `Table` | Accessible table (`Table.Header`/`Table.Body`/`Table.Row`/`Table.Head`/`Table.Cell`/`Table.Caption`/`Table.Footer`) |
| `Breadcrumb` | Breadcrumb trail (`Breadcrumb.List`/`Breadcrumb.Item`/`Breadcrumb.Link`/`Breadcrumb.Page`/`Breadcrumb.Separator`) |
| `Pagination` | Prev/next + page links (pair with `paginationRange`) |
| `Skeleton` | CSS shimmer placeholder |

## Form controls

| Component | Description |
|-----------|-------------|
| `Checkbox` | Checkbox input |
| `RadioGroup` | Grouped radio inputs |
| `Switch` | Toggle switch |
| `Slider` | Range slider |
| `Select` / `NativeSelect` | Searchable select / native `<select>` |
| `Input` | Text input |
| `Textarea` | Multi-line text input |
| `Label` | Form label |
| `Form` | Form container (`Form.Field`/`Form.Input`/`Form.Textarea`/`Form.Submit`) |
| `OTPField` | One-time-password input |
| `PasswordToggleField` | Password input with visibility toggle |
| `Toggle` / `ToggleGroup` | Toggle button(s) |

## Overlays & navigation

| Component | Description |
|-----------|-------------|
| `Accordion` | Collapsible sections |
| `Collapsible` | Collapsible container |
| `Dialog` | Modal dialog |
| `AlertDialog` | Confirmation dialog |
| `Popover` | Floating popover |
| `Sheet` | Edge-anchored dialog/drawer (`Sheet.Trigger`/`Sheet.Content`/`Sheet.Close`/`Sheet.Title`/`Sheet.Description`) |
| `Command` | Filterable command list / palette |
| `HoverCard` | Card revealed on hover |
| `DropdownMenu` | Menu triggered by a button |
| `ContextMenu` | Right-click menu |
| `MenuBar` | Horizontal menu bar |
| `NavigationMenu` | Navigation menu |
| `Tooltip` | Hover tooltip |
| `Toast` | Toast notifications |

## Feedback & display

| Component | Description |
|-----------|-------------|
| `Progress` | Progress indicator |
| `Avatar` | Avatar image/fallback |
| `Separator` | Horizontal/vertical divider |
| `AspectRatio` | Fixed aspect-ratio box |
| `ScrollArea` | Styled scroll container |
| `VisuallyHidden` | Accessible hidden content |
| `Toolbar` | Toolbar of controls |

## Zero-JS compound components

The interactive components use Krate's compiler-erased CSS signals and a
namespaced part API, so they ship **no client JavaScript**. Compose them with
dotted parts and literal `value`s:

```tsx
<Accordion defaultValue="q1">
  <Accordion.Item>
    <Accordion.Trigger value="q1">Question</Accordion.Trigger>
    <Accordion.Content value="q1">Answer</Accordion.Content>
  </Accordion.Item>
</Accordion>
```

This applies to `Tabs`, `Accordion`/`AccordionMultiple`, `Collapsible`,
`RadioGroup`, `ToggleGroup`/`ToggleGroupMultiple`, `DropdownMenu`, `Popover`,
`Dialog`, `AlertDialog`, and `Sheet`. See
[CSS Scopes](/docs/core-concepts/css-signals/) for the model.

`Switch`, `Checkbox`, `Progress`, `Select`, `Tooltip`, and `HoverCard` are
built on native elements or pure CSS and need no signals at all.

Every compound component is used **namespaced** — `Breadcrumb.List`,
`Table.Row`, `Steps.Step`, `Form.Field`, `FileTree.Folder`, `Tabs.Trigger`,
`Dialog.Content`, and so on. The flat exports (`BreadcrumbList`, `TableRow`, …)
still work as deprecated aliases.

For data-driven option sets (options known only at runtime), use the native
`<Select options={…}/>` component rather than a scope component.
