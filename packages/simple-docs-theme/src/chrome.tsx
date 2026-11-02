import type { CSSScope } from "@krate/runtime";
import type { DocsSidebarItem } from "@krate/plugin";

function hasActive(item: DocsSidebarItem): boolean {
  if (item.active) return true;
  if (item.children) {
    for (const child of item.children) {
      if (hasActive(child)) return true;
    }
  }
  return false;
}

function ItemLabel(props: { item: DocsSidebarItem }) {
  return (
    <>
      {props.item.icon && (
        <span class="simple-nav-icon" aria-hidden="true">
          <Icon name={props.item.icon} width="16" height="16" />
        </span>
      )}
      <span class="simple-nav-label">{props.item.title}</span>
      {props.item.badge && (
        <span class={`simple-nav-badge${props.item.badge.variant ? " simple-nav-badge-" + props.item.badge.variant : ""}`}>
          {props.item.badge.text}
        </span>
      )}
    </>
  );
}

function NavItem(props: { item: DocsSidebarItem }) {
  const item = props.item;
  const children = item.children || [];
  const active = hasActive(item);

  if (children.length === 0) {
    if (!item.url) {
      return <span class="simple-nav-link simple-nav-text"><ItemLabel item={item} /></span>;
    }
    return (
      <a class={`simple-nav-link${active ? " is-active" : ""}`} href={item.url} aria-current={active ? "page" : undefined}>
        <ItemLabel item={item} />
      </a>
    );
  }

  const open = !item.collapsible || item.expanded || active;

  return (
    <details class={`simple-nav-section${active ? " is-active" : ""}`} open={open}>
      <summary class="simple-nav-summary">
        <ItemLabel item={item} />
        <span class="simple-nav-chevron" aria-hidden="true">
          <Icon name="lucide:chevron-down" width="14" height="14" />
        </span>
      </summary>
      <div class="simple-nav-children">
        {item.indexURL && (
          <a class="simple-nav-link simple-nav-index" href={item.indexURL}>Overview</a>
        )}
        {children.map((child) => <NavItem item={child} />)}
      </div>
    </details>
  );
}

export function SidebarNav(props: { items: DocsSidebarItem[] }) {
  return (
    <nav class="simple-nav" aria-label="Documentation">
      {props.items.map((item) => <NavItem item={item} />)}
    </nav>
  );
}

export function TOCNav(props: {
  items: { title: string; id: string; depth: number }[];
  label?: string;
}) {
  const label = props.label || "On this page";
  return (
    <details class="simple-toc-mobile">
      <summary>{label}</summary>
      <TOCList items={props.items} />
    </details>
  );
}

export function TOCAside(props: {
  items: { title: string; id: string; depth: number }[];
  label?: string;
}) {
  return (
    <aside class="simple-toc" aria-label={props.label || "On this page"}>
      <div class="simple-toc-title">{props.label || "On this page"}</div>
      <TOCList items={props.items} />
    </aside>
  );
}

function TOCList(props: { items: { title: string; id: string; depth: number }[] }) {
  return (
    <ul class="simple-toc-list">
      {props.items.map((item) => (
        <li class={`simple-toc-item simple-toc-depth-${item.depth}`}>
          <a href={`#${item.id}`}>{item.title}</a>
        </li>
      ))}
    </ul>
  );
}

export function Breadcrumbs(props: { items: { label: string; url: string; isLast: boolean }[] }) {
  if (!props.items || props.items.length === 0) return null;
  return (
    <nav class="simple-breadcrumbs" aria-label="Breadcrumb">
      <ol>
        {props.items.map((item) => (
          <li>
            {item.isLast
              ? <span aria-current="page">{item.label}</span>
              : <a href={item.url}>{item.label}</a>}
          </li>
        ))}
      </ol>
    </nav>
  );
}

export function PrevNext(props: {
  prevTitle?: string;
  prevLink?: string;
  nextTitle?: string;
  nextLink?: string;
}) {
  if (!props.prevLink && !props.nextLink) return null;
  return (
    <nav class="simple-prevnext" aria-label="Page navigation">
      {props.prevLink ? (
        <a class="simple-prevnext-link simple-prevnext-prev" href={props.prevLink}>
          <span class="simple-prevnext-label">Previous</span>
          <span class="simple-prevnext-title">{props.prevTitle || "Previous"}</span>
        </a>
      ) : <span />}
      {props.nextLink ? (
        <a class="simple-prevnext-link simple-prevnext-next" href={props.nextLink}>
          <span class="simple-prevnext-label">Next</span>
          <span class="simple-prevnext-title">{props.nextTitle || "Next"}</span>
        </a>
      ) : <span />}
    </nav>
  );
}

export function SocialLinks(props: { links: { icon?: string; url?: string }[] }) {
  if (!props.links || props.links.length === 0) return null;
  return (
    <div class="simple-social">
      {props.links.map((link) => {
        const icon = link.icon || "lucide:link";
        const label = icon.includes(":") ? icon.slice(icon.indexOf(":") + 1) : icon;
        return (
          <a class="simple-icon-link" href={link.url || "#"} aria-label={label} rel="noreferrer">
            <Icon name={icon} width="18" height="18" />
          </a>
        );
      })}
    </div>
  );
}

export function ThemeSwitch(props: { theme: CSSScope<string> }) {
  const theme = props.theme;
  return (
    <div class="simple-theme-switch" role="group" aria-label="Color theme">
      <button class="simple-theme-option" type="button" title="System theme" aria-label="System theme" {...theme.trigger("auto")}>
        <Icon name="lucide:monitor" width="16" height="16" />
      </button>
      <button class="simple-theme-option" type="button" title="Light theme" aria-label="Light theme" {...theme.trigger("light")}>
        <Icon name="lucide:sun" width="16" height="16" />
      </button>
      <button class="simple-theme-option" type="button" title="Dark theme" aria-label="Dark theme" {...theme.trigger("dark")}>
        <Icon name="lucide:moon" width="16" height="16" />
      </button>
    </div>
  );
}
