import {
  Checkbox,
  Command,
  ContextMenu,
  Dialog,
  HoverCard,
  Label,
  NativeSelect,
  OTPField,
  Pagination,
  Select,
  Slider,
  Switch,
  Tabs,
  Tooltip,
} from '@krate/components';

// Fixture for the keyboard browser test (testdata/components_keyboard.mjs).
const fruit = [
  { value: 'apple', label: 'Apple' },
  { value: 'orange', label: 'Orange' },
  { value: 'lemon', label: 'Lemon', disabled: true },
  { value: 'melon', label: 'Melon' },
];
const panes = [
  { id: 'one', title: 'One' },
  { id: 'two', title: 'Two' },
  { id: 'three', title: 'Three' },
];

export default function ComponentKeyboard() {
  return (
    <main style="padding: 24px; display: grid; gap: 24px; max-width: 640px">
      <h1>Component keyboard fixture</h1>

      <form id="f">
        <OTPField length={4} name="otp" label="Code" onComplete={(v: string) => { (window as any).otpDone = v; }} />
        <Checkbox name="agree" value="yes" id="agree">Agree</Checkbox>
        <Checkbox name="pre" defaultChecked>Pre-checked</Checkbox>
        <Checkbox name="off" disabled>Disabled</Checkbox>
        <Switch name="notify" label="Notifications" id="notify" />
        <Label htmlFor="vol">Volume</Label>
        <Slider id="vol" name="vol" defaultValue={30} />
        <NativeSelect name="native" placeholder="Choose a fruit" options={fruit} label="Native fruit" />
        <Select name="fruit" placeholder="Search a fruit" options={fruit} label="Fruit" defaultValue="orange" />
      </form>

      <Command items={fruit} label="Command" placeholder="Search..." />

      <ContextMenu>
        <ContextMenu.Trigger><div id="cm-area" style="width: 200px; height: 60px; border: 1px dashed">Context area</div></ContextMenu.Trigger>
        <ContextMenu.Content>
          <ContextMenu.Item onSelect={() => { (window as any).picked = ((window as any).picked || '') + 'copy;'; }}>Copy</ContextMenu.Item>
          <ContextMenu.Item disabled onSelect={() => { (window as any).picked = ((window as any).picked || '') + 'cut;'; }}>Cut</ContextMenu.Item>
          <ContextMenu.Item onSelect={() => { (window as any).picked = ((window as any).picked || '') + 'paste;'; }}>Paste</ContextMenu.Item>
        </ContextMenu.Content>
      </ContextMenu>

      <p>
        <Tooltip>
          <Tooltip.Trigger>Plain trigger</Tooltip.Trigger>
          <Tooltip.Content>Plain tip</Tooltip.Content>
        </Tooltip>{' '}
        <Tooltip>
          <Tooltip.Trigger asChild><button id="tip-button" type="button">Real button</button></Tooltip.Trigger>
          <Tooltip.Content>Button tip</Tooltip.Content>
        </Tooltip>{' '}
        <HoverCard>
          <HoverCard.Trigger asChild><a id="card-link" href="#user">@user</a></HoverCard.Trigger>
          <HoverCard.Content>Profile card</HoverCard.Content>
        </HoverCard>
      </p>

      <Pagination page={2} total={9} param="p" />

      <Tabs defaultValue="two">
        <Tabs.List>
          {panes.map((p) => (
            <Tabs.Trigger value={p.id}>{p.title}</Tabs.Trigger>
          ))}
        </Tabs.List>
        {panes.map((p) => (
          <Tabs.Content value={p.id}>Panel {p.title}</Tabs.Content>
        ))}
      </Tabs>

      <Dialog>
        <Dialog.Trigger value="d">Open modal</Dialog.Trigger>
        <Dialog.Content value="d">
          <Dialog.Title>Modal</Dialog.Title>
          <Dialog.Description>Backdrop, inert page, outside click.</Dialog.Description>
          <Dialog.Close>Close</Dialog.Close>
        </Dialog.Content>
      </Dialog>
    </main>
  );
}
