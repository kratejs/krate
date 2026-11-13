import { Toaster, toast } from 'sonner';

// toast() and <Toaster /> share sonner's module state, so they live in one
// island module. A component that only calls toast() is not an island.
export default function Notifier() {
  return (
    <div>
      <button id="t" onClick={() => toast('Saved!', { description: 'all good' })}>toast</button>
      <Toaster />
    </div>
  );
}
