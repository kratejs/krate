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

export function BreadcrumbList(props: { children?: any }) {
  return <ol class="krate-breadcrumb-list">{props.children}</ol>;
}

export function BreadcrumbItem(props: { children?: any }) {
  return <li class="krate-breadcrumb-item">{props.children}</li>;
}

export function BreadcrumbLink(props: { children?: any; href?: string }) {
  return <a class="krate-breadcrumb-link" href={props.href || "#"}>{props.children}</a>;
}

export function BreadcrumbPage(props: { children?: any }) {
  return <span class="krate-breadcrumb-page" aria-current="page">{props.children}</span>;
}

export function BreadcrumbSeparator(props: { children?: any }) {
  return <li class="krate-breadcrumb-separator" role="presentation" aria-hidden="true">{props.children || "/"}</li>;
}
