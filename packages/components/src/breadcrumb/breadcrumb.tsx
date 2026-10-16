import './breadcrumb.css';

export interface BreadcrumbProps {
  children?: any;
  class?: string;
}

export function Breadcrumb(props: BreadcrumbProps) {
  var className = "krate-breadcrumb";
  if (props.class) className += " " + props.class;
  return <nav class={className} aria-label="Breadcrumb">{props.children}</nav>;
}

/** @deprecated Use `Breadcrumb.List`. */
/** @deprecated Use `Breadcrumb.List` instead. */
export function BreadcrumbList(props: { children?: any }) {
  return <ol class="krate-breadcrumb-list">{props.children}</ol>;
}

/** @deprecated Use `Breadcrumb.Item`. */
/** @deprecated Use `Breadcrumb.Item` instead. */
export function BreadcrumbItem(props: { children?: any }) {
  return <li class="krate-breadcrumb-item">{props.children}</li>;
}

/** @deprecated Use `Breadcrumb.Link`. */
/** @deprecated Use `Breadcrumb.Link` instead. */
export function BreadcrumbLink(props: { children?: any; href?: string }) {
  return <a class="krate-breadcrumb-link" href={props.href || "#"}>{props.children}</a>;
}

/** @deprecated Use `Breadcrumb.Page`. */
/** @deprecated Use `Breadcrumb.Page` instead. */
export function BreadcrumbPage(props: { children?: any }) {
  return <span class="krate-breadcrumb-page" aria-current="page">{props.children}</span>;
}

/** @deprecated Use `Breadcrumb.Separator`. */
/** @deprecated Use `Breadcrumb.Separator` instead. */
export function BreadcrumbSeparator(props: { children?: any }) {
  return <li class="krate-breadcrumb-separator" role="presentation" aria-hidden="true">{props.children || "/"}</li>;
}

export interface Breadcrumb {
  List: typeof BreadcrumbList;
  Item: typeof BreadcrumbItem;
  Link: typeof BreadcrumbLink;
  Page: typeof BreadcrumbPage;
  Separator: typeof BreadcrumbSeparator;
}

Breadcrumb.List = BreadcrumbList;
Breadcrumb.Item = BreadcrumbItem;
Breadcrumb.Link = BreadcrumbLink;
Breadcrumb.Page = BreadcrumbPage;
Breadcrumb.Separator = BreadcrumbSeparator;
