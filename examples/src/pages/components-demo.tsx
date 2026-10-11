import {
  Card, CardGrid, LinkCard, LinkButton, Code, FileTree,
  Steps, Tabs, Aside, VisuallyHidden,
  Accordion, AccordionMultiple,
  AlertDialog,
  AspectRatio,
  Avatar,
  Checkbox,
  Collapsible,
  Dialog,
  DropdownMenu,
  Form,
  HoverCard,
  Label,
  Popover,
  Progress,
  RadioGroup,
  Select,
  NativeSelect,
  Separator,
  Switch,
  ToggleGroup,
  ToggleGroupMultiple,
  Tooltip,
} from '@krate/components';

function Section(props: { title?: string; children?: any }) {
  return (
    <div class="demo-section">
      {props.title ? <h2 class="demo-section-title">{props.title}</h2> : null}
      <div class="demo-section-content">{props.children}</div>
    </div>
  );
}

const fruitOptions = [
  { value: 'apple', label: 'Apple' },
  { value: 'orange', label: 'Orange' },
  { value: 'lemon', label: 'Lemon' },
];

export default function App() {
  return (
    <div class="components-demo">
      <h1>All Components Demo</h1>
      <p class="demo-subtitle">Zero-JS components compiled from the built-in CSS signals.</p>

      <Section title="Card / CardGrid / LinkCard">
        <Card title="Basic Card">This is a basic card with a title and body content.</Card>
        <CardGrid>
          <Card title="Grid Card 1">First card in grid</Card>
          <Card title="Grid Card 2">Second card in grid</Card>
        </CardGrid>
        <LinkCard title="Link Card" href="/about" icon="lucide:arrow-right">
          Click this link card to navigate
        </LinkCard>
      </Section>

      <Section title="LinkButton">
        <div style="display: flex; gap: 12px; flex-wrap: wrap;">
          <LinkButton href="/about" variant="primary" size="md">Primary</LinkButton>
          <LinkButton href="/about" variant="secondary" size="md">Secondary</LinkButton>
          <LinkButton href="/about" variant="ghost" size="md">Ghost</LinkButton>
        </div>
      </Section>

      <Section title="Aside">
        <Aside type="note" title="Note">This is a note aside.</Aside>
        <Aside type="tip" title="Tip">This is a tip aside.</Aside>
        <Aside type="warning" title="Warning">This is a warning aside.</Aside>
      </Section>

      <Section title="Code">
        <Code lang="tsx" title="app.tsx" showCopy>
          {`function App() {\n  return <h1>Hello World</h1>;\n}`}
        </Code>
      </Section>

      <Section title="FileTree">
        <FileTree>
          <FileTree.Item>package.json</FileTree.Item>
          <FileTree.Folder name="src" defaultOpen>
            <FileTree.Item>index.tsx</FileTree.Item>
            <FileTree.Folder name="components" defaultOpen>
              <FileTree.Item>button.tsx</FileTree.Item>
            </FileTree.Folder>
          </FileTree.Folder>
        </FileTree>
      </Section>

      <Section title="Steps">
        <Steps>
          <Steps.Step title="Install krate">Run npm install @krate/cli</Steps.Step>
          <Steps.Step title="Create project">Run krate init my-app</Steps.Step>
          <Steps.Step title="Start dev server">Run krate dev</Steps.Step>
        </Steps>
      </Section>

      <Section title="Tabs">
        <Tabs defaultValue="preview">
          <Tabs.List>
            <Tabs.Trigger value="preview">Preview</Tabs.Trigger>
            <Tabs.Trigger value="code">Code</Tabs.Trigger>
            <Tabs.Trigger value="props">Props</Tabs.Trigger>
          </Tabs.List>
          <Tabs.Content value="preview">The preview panel.</Tabs.Content>
          <Tabs.Content value="code">The code panel.</Tabs.Content>
          <Tabs.Content value="props">The props panel.</Tabs.Content>
        </Tabs>
      </Section>

      <Section title="Accordion">
        <Accordion defaultValue="item-1">
          <Accordion.Item>
            <Accordion.Trigger value="item-1">Is it accessible?</Accordion.Trigger>
            <Accordion.Content value="item-1">Yes. It follows the disclosure pattern.</Accordion.Content>
          </Accordion.Item>
          <Accordion.Item>
            <Accordion.Trigger value="item-2">Is it styled?</Accordion.Trigger>
            <Accordion.Content value="item-2">Yes, matching the other components.</Accordion.Content>
          </Accordion.Item>
        </Accordion>
        <AccordionMultiple>
          <AccordionMultiple.Item>
            <AccordionMultiple.Trigger value="a">Multiple: First</AccordionMultiple.Trigger>
            <AccordionMultiple.Content value="a">Open multiple items at once.</AccordionMultiple.Content>
          </AccordionMultiple.Item>
          <AccordionMultiple.Item>
            <AccordionMultiple.Trigger value="b">Multiple: Second</AccordionMultiple.Trigger>
            <AccordionMultiple.Content value="b">A second independent item.</AccordionMultiple.Content>
          </AccordionMultiple.Item>
        </AccordionMultiple>
      </Section>

      <Section title="Collapsible">
        <Collapsible defaultValue="content">
          <Collapsible.Trigger value="content">Toggle collapsible</Collapsible.Trigger>
          <Collapsible.Content value="content">Collapsible content is now visible.</Collapsible.Content>
        </Collapsible>
      </Section>

      <Section title="Dialog / AlertDialog">
        <Dialog>
          <Dialog.Trigger value="dialog">Open Dialog</Dialog.Trigger>
          <Dialog.Content value="dialog">
            <Dialog.Title>Edit Profile</Dialog.Title>
            <Dialog.Description>Make changes to your profile here.</Dialog.Description>
            <Dialog.Close>Close</Dialog.Close>
          </Dialog.Content>
        </Dialog>
        <AlertDialog>
          <AlertDialog.Trigger value="alert">Delete</AlertDialog.Trigger>
          <AlertDialog.Content value="alert">
            <AlertDialog.Title>Are you sure?</AlertDialog.Title>
            <AlertDialog.Description>This action cannot be undone.</AlertDialog.Description>
            <AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
            <AlertDialog.Action>Confirm</AlertDialog.Action>
          </AlertDialog.Content>
        </AlertDialog>
      </Section>

      <Section title="DropdownMenu">
        <DropdownMenu>
          <DropdownMenu.Trigger value="menu">Open Menu</DropdownMenu.Trigger>
          <DropdownMenu.Content value="menu">
            <DropdownMenu.Label>My Account</DropdownMenu.Label>
            <DropdownMenu.Item>Profile</DropdownMenu.Item>
            <DropdownMenu.Separator />
            <DropdownMenu.Item>Sign out</DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu>
      </Section>

      <Section title="Popover / HoverCard / Tooltip">
        <Popover>
          <Popover.Trigger value="pop">Open Popover</Popover.Trigger>
          <Popover.Content value="pop">Popover content.</Popover.Content>
        </Popover>
        <HoverCard>
          <HoverCard.Trigger>@krate</HoverCard.Trigger>
          <HoverCard.Content>A Go-native web framework.</HoverCard.Content>
        </HoverCard>
        <Tooltip>
          <Tooltip.Trigger>Hover me</Tooltip.Trigger>
          <Tooltip.Content>Helpful hint</Tooltip.Content>
        </Tooltip>
      </Section>

      <Section title="AspectRatio / Avatar / Separator">
        <AspectRatio ratio={16 / 9}>
          <div style="background: linear-gradient(135deg, #3b82f6, #8b5cf6); display: flex; align-items: center; justify-content: center; color: white;">
            16:9 Aspect Ratio Box
          </div>
        </AspectRatio>
        <div style="display: flex; gap: 16px; align-items: center;">
          <Avatar size="md" src="https://github.com/shadcn.png" alt="Avatar" fallback="CN" />
          <Avatar size="md" src="" alt="No image" fallback="JD" />
        </div>
        <p>Above separator</p>
        <Separator />
        <p>Below separator</p>
      </Section>

      <Section title="Form controls">
        <Checkbox>Accept terms and conditions</Checkbox>
        <div style="display: flex; align-items: center; gap: 12px; margin-top: 12px;">
          <Switch label="Airplane mode" />
          <Label>Airplane Mode</Label>
        </div>
        <RadioGroup defaultValue="comfortable" label="Density">
          <RadioGroup.Item value="default">Default</RadioGroup.Item>
          <RadioGroup.Item value="comfortable">Comfortable</RadioGroup.Item>
          <RadioGroup.Item value="compact">Compact</RadioGroup.Item>
        </RadioGroup>
        <ToggleGroup defaultValue="bold">
          <ToggleGroup.Item value="bold"><b>B</b></ToggleGroup.Item>
          <ToggleGroup.Item value="italic"><i>I</i></ToggleGroup.Item>
          <ToggleGroup.Item value="underline"><u>U</u></ToggleGroup.Item>
        </ToggleGroup>
        <ToggleGroupMultiple>
          <ToggleGroupMultiple.Item value="bold"><b>B</b></ToggleGroupMultiple.Item>
          <ToggleGroupMultiple.Item value="italic"><i>I</i></ToggleGroupMultiple.Item>
        </ToggleGroupMultiple>
      </Section>

      <Section title="Progress">
        <Progress value={33} label="Upload" />
        <Progress value={66} label="Processing" />
        <Progress value={100} label="Done" />
      </Section>

      <Section title="Select (native + searchable)">
        <NativeSelect placeholder="Choose a fruit..." options={fruitOptions} label="Fruit" />
        <Select placeholder="Search a fruit..." options={fruitOptions} label="Fruit search" />
      </Section>

      <Section title="Form">
        <Form action="/api/subscribe" method="post">
          <Form.Field label="Email" required description="We will never share your email.">
            <Form.Input type="email" placeholder="you@example.com" />
          </Form.Field>
          <Form.Field label="Bio" description="Tell us about yourself.">
            <Form.Textarea placeholder="I am a developer..." rows={4} />
          </Form.Field>
        </Form>
      </Section>

      <Section title="VisuallyHidden">
        <VisuallyHidden>This text is only for screen readers.</VisuallyHidden>
      </Section>
    </div>
  );
}
