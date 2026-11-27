import { onMount, onCleanup, createSignal } from "@krate/runtime";
import { Command } from "@krate/components/command";
import { Dialog } from "@krate/components/dialog";
import { Sheet } from "@krate/components/sheet";
import { Toast } from "@krate/components/toast";
import { toast, openOverlay, closeOverlay } from "@krate/components";
import "./demo.css";

const items = [
  { value: "toast", label: "Show a success toast", group: "Feedback", keywords: "notify message" },
  { value: "error", label: "Show an error toast", group: "Feedback", keywords: "destructive fail" },
  { value: "sheet", label: "Open the settings sheet", group: "Panels", keywords: "drawer preferences" },
  { value: "confirm", label: "Open a dialog", group: "Panels", keywords: "modal" },
  { value: "docs", label: "Go to the docs", group: "Navigate", keywords: "help" },
];

// The palette is a Dialog around a Command list. Dialog, Sheet and Toast are
// CSS-signal components: they open with no JavaScript, and the small
// enhancer adds focus trapping and Escape. Here JS only drives the shortcut.
export default function Demo() {
  const [last, setLast] = createSignal("Nothing yet");

  const run = (value: string) => {
    closeOverlay("#palette");
    setLast(items.find((i) => i.value === value)!.label);
    if (value === "toast") toast({ title: "Saved", description: "Your changes were saved.", variant: "success" });
    if (value === "error") toast({ title: "Something broke", description: "That one was on purpose.", variant: "destructive" });
    if (value === "sheet") openOverlay("#settings");
    if (value === "confirm") openOverlay("#confirm");
    if (value === "docs") location.href = "/docs/";
  };

  onMount(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.altKey && e.code === "KeyP") {
        e.preventDefault();
        openOverlay("#palette");
      }
    };
    window.addEventListener("keydown", onKey);
    onCleanup(() => window.removeEventListener("keydown", onKey));
  });

  return (
    <div class="ov">
      <div class="dk-row">
        <Dialog id="palette" label="Command palette">
          <Dialog.Trigger class="dk-btn dk-btn-primary">Command palette <kbd>Alt P</kbd></Dialog.Trigger>
          <Dialog.Content class="ov-palette">
            <Dialog.Title class="sr-only">Command palette</Dialog.Title>
            <Command items={items} label="Commands" placeholder="Type a command..." emptyText="No matching command." onSelect={run} />
            <Dialog.Close class="ov-close">Close</Dialog.Close>
          </Dialog.Content>
        </Dialog>
        <button type="button" class="dk-btn" onClick={() => toast({ title: "Hello", description: "Toasts stack in the corner." })}>Toast</button>
        <Sheet id="settings" label="Settings">
          <Sheet.Trigger class="dk-btn">Sheet</Sheet.Trigger>
          <Sheet.Content side="right">
            <Sheet.Title>Settings</Sheet.Title>
            <Sheet.Description>A drawer anchored to the edge. Press Escape to close it.</Sheet.Description>
            <Sheet.Close>Done</Sheet.Close>
          </Sheet.Content>
        </Sheet>
        <Dialog id="confirm" label="Confirm">
          <Dialog.Trigger class="dk-btn">Dialog</Dialog.Trigger>
          <Dialog.Content>
            <Dialog.Title>Are you sure?</Dialog.Title>
            <Dialog.Description>Focus stays inside until you close this.</Dialog.Description>
            <Dialog.Close>Close</Dialog.Close>
          </Dialog.Content>
        </Dialog>
      </div>

      <div class="ov-inline">
        <Toast variant="success">
          <Toast.Title>Inline toast</Toast.Title>
          <Toast.Description>A Toast component rendered in the page. Dismiss it with the close button.</Toast.Description>
        </Toast>
      </div>

      <p class="dk-muted ov-last" aria-live="polite">Last action: <strong>{last()}</strong></p>
    </div>
  );
}
