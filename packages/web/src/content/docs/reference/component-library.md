---
title: Component Library
description: The @krate/components UI kit.
order: 3
---

# Component Library

`@krate/components` is a shadcn/ui-style set of accessible UI components built
on the Krate runtime.

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
| `FileTree` | File tree display |
| `Steps` / `Step` | Ordered steps with numbered indicators |
| `Tabs` | Tabbed panels |
| `Table` | Accessible table (`TableHeader`/`TableBody`/`TableRow`/`TableHead`/`TableCell`/`TableCaption`/`TableFooter`) |
| `Breadcrumb` | Breadcrumb trail (`BreadcrumbList`/`BreadcrumbItem`/`BreadcrumbLink`/`BreadcrumbPage`/`BreadcrumbSeparator`) |
| `Pagination` | Prev/next + page links (pair with `paginationRange`) |
| `Skeleton` | CSS shimmer placeholder |

## Form controls

| Component | Description |
|-----------|-------------|
| `Checkbox` | Checkbox input |
| `RadioGroup` | Grouped radio inputs |
| `Switch` | Toggle switch |
| `Slider` | Range slider |
| `Select` | Select dropdown |
| `Input` | Text input |
| `Textarea` | Multi-line text input |
| `Label` | Form label |
| `Form` | Form container |
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
| `Sheet` | Edge-anchored dialog/drawer (`SheetTrigger`/`SheetContent`/`SheetClose`/`SheetTitle`/`SheetDescription`) |
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

All components follow Radix-style accessibility patterns and integrate with
Krate's signal-based event system for interactivity.
