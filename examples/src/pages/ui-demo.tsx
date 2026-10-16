import {
  Button,
  Badge,
  Input,
  Textarea,
  Skeleton,
  Alert,
  Table,
  Breadcrumb,
  Pagination,
  Sheet,
  Command,
} from '@krate/components';

const commandItems = [
  { value: 'home', label: 'Go home', group: 'Navigation' },
  { value: 'about', label: 'About Krate', group: 'Navigation' },
  { value: 'docs', label: 'Open the docs', group: 'Navigation' },
  { value: 'theme', label: 'Toggle theme', group: 'Actions' },
];

export default function UIDemo() {
  return (
    <div class="ui-demo">
      <h1>UI Components</h1>

      <section>
        <h2>Buttons</h2>
        <Button>Primary</Button>
        <Button variant="secondary">Secondary</Button>
        <Button variant="outline">Outline</Button>
        <Button variant="ghost">Ghost</Button>
        <Button variant="destructive">Destructive</Button>
        <Button size="sm">Small</Button>
        <Button size="lg">Large</Button>
        <Button disabled>Disabled</Button>
      </section>

      <section>
        <h2>Badges</h2>
        <Badge>Default</Badge>
        <Badge variant="secondary">Secondary</Badge>
        <Badge variant="outline">Outline</Badge>
        <Badge variant="success">Success</Badge>
        <Badge variant="warning">Warning</Badge>
        <Badge variant="destructive">Destructive</Badge>
      </section>

      <section>
        <h2>Inputs</h2>
        <Input placeholder="Your name" />
        <Textarea placeholder="Tell us more" rows={3} />
      </section>

      <section>
        <h2>Alerts</h2>
        <Alert variant="info" title="Heads up">This is an informational alert.</Alert>
        <Alert variant="success" title="Saved">Your changes were saved.</Alert>
        <Alert variant="destructive" title="Error">Something went wrong.</Alert>
      </section>

      <section>
        <h2>Breadcrumb</h2>
        <Breadcrumb>
          <Breadcrumb.List>
            <Breadcrumb.Item><Breadcrumb.Link href="/">Home</Breadcrumb.Link></Breadcrumb.Item>
            <Breadcrumb.Separator />
            <Breadcrumb.Item><Breadcrumb.Link href="/docs">Docs</Breadcrumb.Link></Breadcrumb.Item>
            <Breadcrumb.Separator />
            <Breadcrumb.Item><Breadcrumb.Page>Components</Breadcrumb.Page></Breadcrumb.Item>
          </Breadcrumb.List>
        </Breadcrumb>
      </section>

      <section>
        <h2>Table</h2>
        <Table>
          <Table.Header>
            <Table.Row>
              <Table.Head>Name</Table.Head>
              <Table.Head>Role</Table.Head>
              <Table.Head>Status</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            <Table.Row>
              <Table.Cell>Ada</Table.Cell>
              <Table.Cell>Engineer</Table.Cell>
              <Table.Cell><Badge variant="success">Active</Badge></Table.Cell>
            </Table.Row>
            <Table.Row>
              <Table.Cell>Grace</Table.Cell>
              <Table.Cell>Compiler</Table.Cell>
              <Table.Cell><Badge variant="warning">Away</Badge></Table.Cell>
            </Table.Row>
          </Table.Body>
          <Table.Caption>Team members</Table.Caption>
        </Table>
      </section>

      <section>
        <h2>Pagination</h2>
        <Pagination page={3} total={10} hrefPrefix="/ui-demo?page=" />
      </section>

      <section>
        <h2>Skeleton</h2>
        <Skeleton class="w-full h-4" />
        <Skeleton class="w-1/2 h-4" />
      </section>

      <section>
        <h2>Sheet</h2>
        <Sheet>
          <Sheet.Trigger value="settings">Open settings</Sheet.Trigger>
          <Sheet.Content value="settings" side="right">
            <Sheet.Title>Settings</Sheet.Title>
            <Sheet.Description>Adjust your preferences.</Sheet.Description>
            <Sheet.Close>Close</Sheet.Close>
          </Sheet.Content>
        </Sheet>
      </section>

      <section>
        <h2>Command</h2>
        <Command items={commandItems} placeholder="Search commands..." />
      </section>
    </div>
  );
}
