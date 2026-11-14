import { Toaster, toast } from 'sonner';

// toast() and <Toaster /> in one module. SaveButton.tsx calls toast() alone and
// pages/toastlayout renders the Toaster from a layout.
export default function Notifier() {
  return (
    <div>
      <button id="t" onClick={() => toast('Saved!', { description: 'all good' })}>toast</button>
      <Toaster />
    </div>
  );
}
