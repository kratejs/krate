import {
  Command,
  Select,
  ContextMenu,
  OTPField,
  PasswordToggleField,
  Pagination,
} from '@krate/components';

const commandItems = [
  { value: 'home', label: 'Go home', group: 'Navigation' },
  { value: 'about', label: 'About Krate', group: 'Navigation' },
  { value: 'theme', label: 'Toggle theme', group: 'Actions' },
];

export default function InteractiveDemo() {
  return (
    <div class="interactive-demo">
      <h1>Interactive components (krate runtime)</h1>

      <section>
        <h2>Searchable select</h2>
        <Select
          label="Fruit"
          placeholder="Search a fruit..."
          options={[
            { value: 'apple', label: 'Apple' },
            { value: 'orange', label: 'Orange' },
            { value: 'lemon', label: 'Lemon' },
            { value: 'mango', label: 'Mango' },
          ]}
        />
      </section>

      <section>
        <h2>Command</h2>
        <Command items={commandItems} placeholder="Search commands..." />
      </section>

      <section>
        <h2>Pagination</h2>
        <Pagination page={3} total={10} hrefPrefix="/interactive-demo?page=" />
      </section>

      <section>
        <h2>Context menu</h2>
        <ContextMenu>
          <ContextMenu.Trigger>
            <div style="border: 2px dashed var(--krate-border); border-radius: 8px; padding: 40px; text-align: center;">
              Right-click here
            </div>
          </ContextMenu.Trigger>
          <ContextMenu.Content>
            <ContextMenu.Label>Actions</ContextMenu.Label>
            <ContextMenu.Item>Copy</ContextMenu.Item>
            <ContextMenu.Item>Paste</ContextMenu.Item>
            <ContextMenu.Separator />
            <ContextMenu.Item>Delete</ContextMenu.Item>
          </ContextMenu.Content>
        </ContextMenu>
      </section>

      <section>
        <h2>OTP field</h2>
        <OTPField length={6} />
      </section>

      <section>
        <h2>Password toggle</h2>
        <PasswordToggleField placeholder="Enter your password" />
      </section>
    </div>
  );
}
