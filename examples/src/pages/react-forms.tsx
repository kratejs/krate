import { useState } from 'react';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';

export default function Page() {
  const [name, setName] = useState('');
  const [role, setRole] = useState('engineer');
  const [subscribe, setSubscribe] = useState(false);
  const [plan, setPlan] = useState('team');
  const [notes, setNotes] = useState('');
  const [submitted, setSubmitted] = useState('Not submitted');

  return (
    <div class="mx-auto max-w-2xl space-y-6 px-6 py-10">
      <header class="space-y-2">
        <p class="text-xs font-semibold uppercase tracking-wide text-indigo-300">Conformance corpus</p>
        <h1 class="text-3xl font-bold text-slate-100">React forms</h1>
        <p class="text-sm text-slate-400">
          Controlled text, select, checkbox, radio, textarea, submit handling, and event values.
        </p>
      </header>

      <form
        data-testid="react-form"
        class="space-y-5 rounded-xl border border-slate-700 bg-slate-900/60 p-5"
        onSubmit={(event: any) => {
          event.preventDefault();
          setSubmitted(`${name || 'anonymous'}|${role}|${subscribe ? 'subscribed' : 'muted'}|${plan}|${notes || 'no notes'}`);
        }}
      >
        <div class="grid gap-4 sm:grid-cols-2">
          <label class="space-y-1">
            <span class="text-sm font-medium text-slate-200">Name</span>
            <Input
              value={name}
              placeholder="Ada Lovelace"
              onChange={(event: any) => setName(event.target.value)}
            />
          </label>

          <label class="space-y-1">
            <span class="text-sm font-medium text-slate-200">Role</span>
            <select
              data-testid="form-role"
              class="h-9 w-full rounded-md border border-slate-700 bg-slate-950 px-3 text-sm text-slate-100"
              value={role}
              onChange={(event: any) => setRole(event.target.value)}
            >
              <option value="engineer">Engineer</option>
              <option value="designer">Designer</option>
              <option value="maintainer">Maintainer</option>
            </select>
          </label>
        </div>

        <label class="flex items-center gap-2 text-sm text-slate-200">
          <input
            data-testid="form-subscribe"
            type="checkbox"
            checked={subscribe}
            onChange={(event: any) => setSubscribe(event.target.checked)}
          />
          Subscribe to release notes
        </label>

        <fieldset class="space-y-2">
          <legend class="text-sm font-medium text-slate-200">Plan</legend>
          {['solo', 'team', 'enterprise'].map((value) => (
            <label class="flex items-center gap-2 text-sm text-slate-300">
              <input
                data-testid={`form-plan-${value}`}
                type="radio"
                name="plan"
                value={value}
                checked={plan === value}
                onChange={() => setPlan(value)}
              />
              {value}
            </label>
          ))}
        </fieldset>

        <label class="block space-y-1">
          <span class="text-sm font-medium text-slate-200">Notes</span>
          <textarea
            data-testid="form-notes"
            class="min-h-24 w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-slate-100"
            value={notes}
            placeholder="Anything else?"
            onChange={(event: any) => setNotes(event.target.value)}
          />
        </label>

        <div class="flex flex-wrap items-center gap-3">
          <Button type="submit">
            Submit
          </Button>
          <p data-testid="form-result" class="text-sm text-slate-300">
            {submitted}
          </p>
        </div>
      </form>
    </div>
  );
}
