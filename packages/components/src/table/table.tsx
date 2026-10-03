import './table.css';

export interface TableProps {
  children?: any;
  class?: string;
}

export function Table(props: TableProps) {
  var className = "krate-table";
  if (props.class) className += " " + props.class;
  return (
    <div class="krate-table-wrap">
      <table class={className}>{props.children}</table>
    </div>
  );
}

export function TableHeader(props: { children?: any }) {
  return <thead class="krate-table-header">{props.children}</thead>;
}

export function TableBody(props: { children?: any }) {
  return <tbody class="krate-table-body">{props.children}</tbody>;
}

export function TableFooter(props: { children?: any }) {
  return <tfoot class="krate-table-footer">{props.children}</tfoot>;
}

export function TableRow(props: { children?: any }) {
  return <tr class="krate-table-row">{props.children}</tr>;
}

export function TableHead(props: { children?: any; class?: string }) {
  var className = "krate-table-head";
  if (props.class) className += " " + props.class;
  return <th class={className} scope="col">{props.children}</th>;
}

export function TableCell(props: { children?: any; class?: string; colspan?: number }) {
  var className = "krate-table-cell";
  if (props.class) className += " " + props.class;
  return <td class={className} colspan={props.colspan}>{props.children}</td>;
}

export function TableCaption(props: { children?: any }) {
  return <caption class="krate-table-caption">{props.children}</caption>;
}
