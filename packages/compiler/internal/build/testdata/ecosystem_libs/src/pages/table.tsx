import { useState } from 'react';
import { useReactTable, getCoreRowModel, getSortedRowModel, flexRender, createColumnHelper } from '@tanstack/react-table';

type P = { name: string; age: number };
const helper = createColumnHelper<P>();
const columns = [helper.accessor('name', { header: 'Name' }), helper.accessor('age', { header: 'Age' })];
const data: P[] = [{ name: 'Ada', age: 36 }, { name: 'Bob', age: 25 }, { name: 'Cy', age: 41 }];

export default function Page() {
  const [sorting, setSorting] = useState<any>([]);
  const table = useReactTable({ data, columns, state: { sorting }, onSortingChange: setSorting, getCoreRowModel: getCoreRowModel(), getSortedRowModel: getSortedRowModel() });
  return (
    <main>
      <table>
        <thead>{table.getHeaderGroups().map((hg) => (
          <tr key={hg.id}>{hg.headers.map((h) => (
            <th key={h.id} id={'th-' + h.id} onClick={h.column.getToggleSortingHandler()}>{flexRender(h.column.columnDef.header, h.getContext())}</th>
          ))}</tr>
        ))}</thead>
        <tbody>{table.getRowModel().rows.map((r) => (
          <tr key={r.id}>{r.getVisibleCells().map((c) => <td key={c.id}>{flexRender(c.column.columnDef.cell, c.getContext())}</td>)}</tr>
        ))}</tbody>
      </table>
    </main>
  );
}
