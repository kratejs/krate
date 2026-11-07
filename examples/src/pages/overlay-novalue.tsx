import { Dialog, DropdownMenu, Popover, Sheet } from "@krate/components";

// Triggers and contents written without a `value` still open each other.
export default function OverlayNoValue() {
  return (
    <main>
      <h1>Overlays without values</h1>
      <Dialog>
        <Dialog.Trigger>Open dialog</Dialog.Trigger>
        <Dialog.Content>
          <Dialog.Title>Dialog title</Dialog.Title>
          <Dialog.Close>Close dialog</Dialog.Close>
        </Dialog.Content>
      </Dialog>
      <Sheet>
        <Sheet.Trigger>Open sheet</Sheet.Trigger>
        <Sheet.Content>
          <Sheet.Title>Sheet title</Sheet.Title>
          <Sheet.Close>Close sheet</Sheet.Close>
        </Sheet.Content>
      </Sheet>
      <Popover>
        <Popover.Trigger>Open popover</Popover.Trigger>
        <Popover.Content>Popover body</Popover.Content>
      </Popover>
      <DropdownMenu>
        <DropdownMenu.Trigger>Open menu</DropdownMenu.Trigger>
        <DropdownMenu.Content>
          <DropdownMenu.Item>Menu item</DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu>
    </main>
  );
}
