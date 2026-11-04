import { createSignal } from '@krate/runtime';
import { Dialog, Sheet, openOverlay, closeOverlay, toast } from '@krate/components';

export default function OverlayControl() {
  const [open, setOpen] = createSignal(false);
  const [changes, setChanges] = createSignal(0);
  return (
    <div class="overlay-control">
      <h1>Overlay control</h1>

      <section>
        <h2>Controlled</h2>
        <button id="ctl-open" type="button" onClick={() => setOpen(true)}>Open controlled</button>
        <span id="ctl-state">{open() ? 'open' : 'closed'}</span>
        <span id="ctl-changes">{changes()}</span>
        <Dialog
          open={open()}
          onOpenChange={(next: boolean) => {
            setOpen(next);
            setChanges(changes() + 1);
          }}
        >
          <Dialog.Content value="c">
            <Dialog.Title>Controlled dialog</Dialog.Title>
            <Dialog.Description>Driven by a signal.</Dialog.Description>
            <Dialog.Close>Close</Dialog.Close>
          </Dialog.Content>
        </Dialog>
      </section>

      <section>
        <h2>Open by default</h2>
        <Dialog defaultOpen id="default-open">
          <Dialog.Trigger value="c">Reopen</Dialog.Trigger>
          <Dialog.Content value="c">
            <Dialog.Title>Open on load</Dialog.Title>
            <Dialog.Close>Close</Dialog.Close>
          </Dialog.Content>
        </Dialog>
      </section>

      <section>
        <h2>Imperative</h2>
        <Sheet id="imp">
          <Sheet.Content value="c">
            <Sheet.Title>Imperative sheet</Sheet.Title>
            <Sheet.Close>Close</Sheet.Close>
          </Sheet.Content>
        </Sheet>
        <button id="imp-open" type="button" onClick={() => openOverlay('#imp')}>Open sheet</button>
        <button id="imp-close" type="button" onClick={() => closeOverlay('#imp')}>Close sheet</button>
        <button id="toast-btn" type="button" onClick={() => toast({ title: 'Saved', description: 'Your changes were saved.', duration: 0 })}>Toast</button>
      </section>
    </div>
  );
}
