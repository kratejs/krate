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

/** @deprecated Use `Table.Header` instead. */
export function TableHeader(props: { children?: any }) {
  return <thead class="krate-table-header">{props.children}</thead>;
}

/** @deprecated Use `Table.Body` instead. */
export function TableBody(props: { children?: any }) {
  return <tbody class="krate-table-body">{props.children}</tbody>;
}

/** @deprecated Use `Table.Footer` instead. */
export function TableFooter(props: { children?: any }) {
  return <tfoot class="krate-table-footer">{props.children}</tfoot>;
}

/** @deprecated Use `Table.Row` instead. */
export function TableRow(props: { children?: any }) {
  return <tr class="krate-table-row">{props.children}</tr>;
}

/** @deprecated Use `Table.Head` instead. */
export function TableHead(props: { children?: any; class?: string }) {
  var className = "krate-table-head";
  if (props.class) className += " " + props.class;
  return <th class={className} scope="col">{props.children}</th>;
}

/** @deprecated Use `Table.Cell` instead. */
export function TableCell(props: { children?: any; class?: string; colspan?: number }) {
  var className = "krate-table-cell";
  if (props.class) className += " " + props.class;
  return <td class={className} colspan={props.colspan}>{props.children}</td>;
}

/** @deprecated Use `Table.Caption` instead. */
export function TableCaption(props: { children?: any }) {
  return <caption class="krate-table-caption">{props.children}</caption>;
}

export interface Table {
  Header: typeof TableHeader;
  Body: typeof TableBody;
  Footer: typeof TableFooter;
  Row: typeof TableRow;
  Head: typeof TableHead;
  Cell: typeof TableCell;
  Caption: typeof TableCaption;
}

Table.Header = TableHeader;
Table.Body = TableBody;
Table.Footer = TableFooter;
Table.Row = TableRow;
Table.Head = TableHead;
Table.Cell = TableCell;
Table.Caption = TableCaption;
