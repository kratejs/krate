import { Command } from 'cmdk';
export default function Page() {
  return (
    <main>
      <Command label="Menu">
        <Command.Input id="q" placeholder="Search..." />
        <Command.List>
          <Command.Empty id="empty">No results.</Command.Empty>
          <Command.Item id="a" value="apple">Apple</Command.Item>
          <Command.Item id="b" value="banana">Banana</Command.Item>
          <Command.Item id="c" value="cherry">Cherry</Command.Item>
        </Command.List>
      </Command>
    </main>
  );
}
