import { useMemo, useState } from 'react';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';

type Report = {
  id: number,
  name: string,
  owner: string,
  status: 'healthy' | 'warning' | 'failed',
};


function StatusBadge(props: { status: Report['status'] }) {
  const tone =
    props.status === 'healthy'
      ? 'bg-emerald-600 text-white'
      : props.status === 'warning'
        ? 'bg-amber-500 text-slate-950'
        : 'bg-red-600 text-white';
  return <span class={`rounded-full px-2 py-0.5 text-xs font-semibold ${tone}`}>{props.status}</span>;
}

export default function Page() {
  const [query, setQuery] = useState('');
  const [reports, setReports] = useState<Report[]>([
    { id: 1, name: 'Alpha release', owner: 'Ada', status: 'healthy' },
    { id: 2, name: 'Beta migration', owner: 'Grace', status: 'warning' },
    { id: 3, name: 'Gamma audit', owner: 'Linus', status: 'failed' },
  ]);

  const visible = useMemo(
    () =>
      reports().filter((report) =>
        `${report.name} ${report.owner} ${report.status}`.toLowerCase().includes(query().toLowerCase())
      ),
    [reports, query]
  );

  const addReport = () => {
    const id = reports().length + 1;
    setReports([...reports, { id, name: `New report ${id}`, owner: 'Krate', status: 'healthy' }]);
  };

  return (
    <div class="mx-auto max-w-5xl space-y-6 px-6 py-10">
      <header class="space-y-2">
        <p class="text-xs font-semibold uppercase tracking-wide text-indigo-300">Conformance corpus</p>
        <h1 class="text-3xl font-bold text-slate-100">React dashboard</h1>
        <p class="max-w-2xl text-sm text-slate-400">
          React source components, shadcn-style primitives, controlled inputs, derived lists, and
          dynamic updates.
        </p>
      </header>

      <div class="grid gap-4 md:grid-cols-3">
        <section class="rounded-xl border border-slate-700 bg-slate-900/60 p-5">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-400">Reports</h2>
          <p data-testid="dashboard-total" class="mt-3 text-3xl font-semibold text-slate-100">
            {reports().length}
          </p>
        </section>
        <section class="rounded-xl border border-slate-700 bg-slate-900/60 p-5">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-400">Healthy</h2>
          <p data-testid="dashboard-healthy" class="mt-3 text-3xl font-semibold text-emerald-400">
            {reports().filter((report) => report.status === 'healthy').length}
          </p>
        </section>
        <section class="rounded-xl border border-slate-700 bg-slate-900/60 p-5">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-400">Needs attention</h2>
          <p data-testid="dashboard-attention" class="mt-3 text-3xl font-semibold text-amber-400">
            {reports().filter((report) => report.status !== 'healthy').length}
          </p>
        </section>
      </div>

      <section class="rounded-xl border border-slate-700 bg-slate-900/60 p-5">
        <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-400">Report queue</h2>
        <div class="mt-3">
        <div class="flex flex-wrap items-end gap-3">
          <label class="grow">
            <span class="mb-1 block text-xs font-medium text-slate-400">Filter</span>
            <Input
              value={query}
              placeholder="Filter reports..."
              onChange={(event: any) => setQuery(event.target.value)}
            />
          </label>
          <Button onClick={addReport}>
            Add report
          </Button>
        </div>

        <ul class="mt-5 divide-y divide-slate-800">
          {visible().map((report) => (
            <li
              key={report.id}
              data-testid="dashboard-row"
              data-report={report.name}
              class="flex items-center justify-between gap-4 py-3"
            >
              <div>
                <p class="text-sm font-medium text-slate-200">{report.name}</p>
                <p class="text-xs text-slate-500">{report.owner}</p>
              </div>
              <StatusBadge status={report.status} />
            </li>
          ))}
        </ul>
        {visible().length === 0 ? (
          <p data-testid="dashboard-empty" class="mt-4 text-sm text-slate-400">
            No reports match.
          </p>
        ) : null}
        </div>
      </section>
    </div>
  );
}
