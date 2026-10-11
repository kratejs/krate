import {
  Accordion,
  AccordionMultiple,
  Collapsible,
  DropdownMenu,
  Popover,
  Dialog,
  RadioGroup,
  Switch,
  Checkbox,
  Toggle,
  ToggleGroup,
  ToggleGroupMultiple,
  Progress,
  NativeSelect,
  Tooltip,
  HoverCard,
  FileTree,
  MenuBar,
  NavigationMenu,
  Toolbar,
  Slider,
  Toast,
  ScrollArea,
  Form,
  FormField,
  FormInput,
  FormTextarea,
  FormSubmit,
} from '@krate/components';

const fruitOptions = [
  { value: 'apple', label: 'Apple' },
  { value: 'orange', label: 'Orange' },
  { value: 'lemon', label: 'Lemon' },
];

export default function ZeroJSDemo() {
  return (
    <div class="zerojs-demo">
      <h1>Zero-JS components</h1>

      <section>
        <h2>Tabs</h2>
        <Tabs defaultValue="a">
          <Tabs.List>
            <Tabs.Trigger value="a">A</Tabs.Trigger>
            <Tabs.Trigger value="b">B</Tabs.Trigger>
          </Tabs.List>
          <Tabs.Content value="a">Panel A</Tabs.Content>
          <Tabs.Content value="b">Panel B</Tabs.Content>
        </Tabs>
      </section>

      <section>
        <h2>Accordion (single / multiple)</h2>
        <Accordion defaultValue="a1">
          <Accordion.Item>
            <Accordion.Trigger value="a1">What is Krate?</Accordion.Trigger>
            <Accordion.Content value="a1">A Go-native web framework.</Accordion.Content>
          </Accordion.Item>
          <Accordion.Item>
            <Accordion.Trigger value="a2">Is it zero-JS?</Accordion.Trigger>
            <Accordion.Content value="a2">These panels need no JavaScript.</Accordion.Content>
          </Accordion.Item>
        </Accordion>
        <AccordionMultiple>
          <AccordionMultiple.Item>
            <AccordionMultiple.Trigger value="m1">First</AccordionMultiple.Trigger>
            <AccordionMultiple.Content value="m1">First panel.</AccordionMultiple.Content>
          </AccordionMultiple.Item>
          <AccordionMultiple.Item>
            <AccordionMultiple.Trigger value="m2">Second</AccordionMultiple.Trigger>
            <AccordionMultiple.Content value="m2">Second panel.</AccordionMultiple.Content>
          </AccordionMultiple.Item>
        </AccordionMultiple>
      </section>

      <section>
        <h2>Collapsible</h2>
        <Collapsible defaultValue="c1">
          <Collapsible.Trigger value="c1">Toggle</Collapsible.Trigger>
          <Collapsible.Content value="c1">Collapsible body.</Collapsible.Content>
        </Collapsible>
      </section>

      <section>
        <h2>Radio / Toggle groups</h2>
        <RadioGroup defaultValue="comfortable">
          <RadioGroup.Item value="default">Default</RadioGroup.Item>
          <RadioGroup.Item value="comfortable">Comfortable</RadioGroup.Item>
          <RadioGroup.Item value="compact">Compact</RadioGroup.Item>
        </RadioGroup>
        <ToggleGroup defaultValue="bold">
          <ToggleGroup.Item value="bold"><b>B</b></ToggleGroup.Item>
          <ToggleGroup.Item value="italic"><i>I</i></ToggleGroup.Item>
        </ToggleGroup>
        <ToggleGroupMultiple>
          <ToggleGroupMultiple.Item value="bold"><b>B</b></ToggleGroupMultiple.Item>
          <ToggleGroupMultiple.Item value="italic"><i>I</i></ToggleGroupMultiple.Item>
        </ToggleGroupMultiple>
      </section>

      <section>
        <h2>Switch / Checkbox / Toggle</h2>
        <Switch label="Airplane mode" />
        <Checkbox>Accept terms</Checkbox>
        <Toggle>Toggle me</Toggle>
      </section>

      <section>
        <h2>Menus</h2>
        <DropdownMenu>
          <DropdownMenu.Trigger value="menu">Open menu</DropdownMenu.Trigger>
          <DropdownMenu.Content value="menu">
            <DropdownMenu.Label>Account</DropdownMenu.Label>
            <DropdownMenu.Item>Profile</DropdownMenu.Item>
            <DropdownMenu.Separator />
            <DropdownMenu.Item>Sign out</DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu>
        <MenuBar>
          <MenuBar.Trigger value="file">File</MenuBar.Trigger>
          <MenuBar.Content value="file">
            <MenuBar.Item>New</MenuBar.Item>
            <MenuBar.Item>Open</MenuBar.Item>
          </MenuBar.Content>
          <MenuBar.Trigger value="edit">Edit</MenuBar.Trigger>
          <MenuBar.Content value="edit">
            <MenuBar.Item>Undo</MenuBar.Item>
            <MenuBar.Item>Redo</MenuBar.Item>
          </MenuBar.Content>
        </MenuBar>
        <NavigationMenu>
          <NavigationMenu.Trigger value="start">Getting Started</NavigationMenu.Trigger>
          <NavigationMenu.Content value="start">
            <NavigationMenu.Link href="/about">Introduction</NavigationMenu.Link>
            <NavigationMenu.Link href="/about">Installation</NavigationMenu.Link>
          </NavigationMenu.Content>
        </NavigationMenu>
      </section>

      <section>
        <h2>Overlays</h2>
        <Popover>
          <Popover.Trigger value="p">Open popover</Popover.Trigger>
          <Popover.Content value="p">Popover content.</Popover.Content>
        </Popover>
        <Dialog>
          <Dialog.Trigger value="d">Open dialog</Dialog.Trigger>
          <Dialog.Content value="d">
            <Dialog.Title>Edit profile</Dialog.Title>
            <Dialog.Description>Make changes and save.</Dialog.Description>
            <Dialog.Close>Close</Dialog.Close>
          </Dialog.Content>
        </Dialog>
        <Tooltip>
          <Tooltip.Trigger>Hover me</Tooltip.Trigger>
          <Tooltip.Content>Helpful hint</Tooltip.Content>
        </Tooltip>
        <HoverCard>
          <HoverCard.Trigger>@krate</HoverCard.Trigger>
          <HoverCard.Content>A Go-native web framework.</HoverCard.Content>
        </HoverCard>
        <Toast>
          <Toast.Title>Saved</Toast.Title>
          <Toast.Description>Your changes were saved.</Toast.Description>
        </Toast>
      </section>

      <section>
        <h2>Toolbar / Slider / Progress / ScrollArea</h2>
        <Toolbar label="Formatting">
          <Toolbar.Group>
            <Toolbar.Button>B</Toolbar.Button>
            <Toolbar.Button>I</Toolbar.Button>
          </Toolbar.Group>
          <Toolbar.Separator />
          <Toolbar.Link href="/about">Link</Toolbar.Link>
        </Toolbar>
        <Slider min={0} max={100} step={5} defaultValue={40} label="Volume" />
        <Progress value={66} label="Upload" />
        <ScrollArea maxHeight="120px">
          <p>Scroll content 1.</p>
          <p>Scroll content 2.</p>
          <p>Scroll content 3.</p>
          <p>Scroll content 4.</p>
          <p>Scroll content 5.</p>
        </ScrollArea>
      </section>

      <section>
        <h2>FileTree</h2>
        <FileTree>
          <FileTree.Item>package.json</FileTree.Item>
          <FileTree.Folder name="src" defaultOpen={true}>
            <FileTree.Item>index.tsx</FileTree.Item>
            <FileTree.Folder name="components">
              <FileTree.Item>button.tsx</FileTree.Item>
            </FileTree.Folder>
          </FileTree.Folder>
        </FileTree>
      </section>

      <section>
        <h2>Select / Pagination / Form</h2>
        <NativeSelect placeholder="Choose a fruit" options={fruitOptions} label="Fruit" />
        <Form action="/api/subscribe" method="post">
          <FormField label="Email" required description="We will never share your email.">
            <FormInput type="email" placeholder="you@example.com" />
          </FormField>
          <FormField label="Bio">
            <FormTextarea placeholder="Tell us about yourself..." rows={3} />
          </FormField>
          <FormSubmit>Save</FormSubmit>
        </Form>
      </section>
    </div>
  );
}
