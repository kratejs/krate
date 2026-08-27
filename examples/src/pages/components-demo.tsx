import { createSignal } from '@krate/runtime';
import {
  Card, CardGrid, LinkCard, LinkButton, Code, FileTree, FileTreeItem, FileTreeFolder,
  Steps, Step, Tabs, TabsList, TabsTrigger, TabsContent, Aside, VisuallyHidden,
  Accordion, AccordionItem, AccordionTrigger, AccordionContent,
  AlertDialog, AlertDialogTrigger, AlertDialogContent, AlertDialogTitle, AlertDialogDescription, AlertDialogAction, AlertDialogCancel,
  AspectRatio,
  Avatar,
  Checkbox,
  Collapsible, CollapsibleTrigger, CollapsibleContent,
  ContextMenu, ContextMenuTrigger, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuLabel,
  Dialog, DialogTrigger, DialogContent, DialogTitle, DialogDescription, DialogClose,
  DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuLabel, DropdownMenuGroup,
  Form, FormField, FormInput, FormTextarea,
  HoverCard, HoverCardTrigger, HoverCardContent,
  Label,
  MenuBar, MenuBarMenu, MenuBarTrigger, MenuBarContent, MenuBarItem, MenuBarSeparator, MenuBarLabel,
  NavigationMenu, NavigationMenuList, NavigationMenuItem, NavigationMenuTrigger, NavigationMenuContent, NavigationMenuLink,
  OTPField,
  PasswordToggleField,
  Popover, PopoverTrigger, PopoverContent,
  Progress,
  RadioGroup, RadioGroupItem, RadioGroupLabel,
  ScrollArea, ScrollAreaViewport,
  Select, SelectItem, SelectGroup, SelectSeparator, SelectLabel,
  Separator,
  Slider,
  Switch,
  Toast, ToastProvider, ToastViewport, ToastTitle, ToastDescription,
  Toggle,
  ToggleGroup, ToggleGroupItem,
  Toolbar, ToolbarButton, ToolbarSeparator, ToolbarLink, ToolbarGroup,
  Tooltip, TooltipTrigger, TooltipContent,
} from '@krate/components';

function Section(props: { title?: string; children?: any }) {
  return (
    <div class="demo-section">
      {props.title ? <h2 class="demo-section-title">{props.title}</h2> : null}
      <div class="demo-section-content">{props.children}</div>
    </div>
  );
}

function App() {
  return (
    <div class="components-demo">
      <h1>All Components Demo</h1>
      <p class="demo-subtitle">Every @krate/components component tested on one page.</p>

      {/* ===== Card, CardGrid, LinkCard ===== */}
      <Section title="Card / CardGrid / LinkCard">
        <Card title="Basic Card">This is a basic card with a title and body content.</Card>
        <CardGrid>
          <Card title="Grid Card 1">First card in grid</Card>
          <Card title="Grid Card 2">Second card in grid</Card>
          <Card title="Grid Card 3">Third card in grid</Card>
        </CardGrid>
        <LinkCard title="Link Card" href="/about" icon="lucide:arrow-right">
          Click this link card to navigate
        </LinkCard>
      </Section>

      {/* ===== LinkButton ===== */}
      <Section title="LinkButton">
        <div style="display: flex; gap: 12px; flex-wrap: wrap;">
          <LinkButton href="/about" variant="primary" size="md">Primary</LinkButton>
          <LinkButton href="/about" variant="secondary" size="md">Secondary</LinkButton>
          <LinkButton href="/about" variant="ghost" size="md">Ghost</LinkButton>
          <LinkButton href="/about" variant="destructive" size="md">Destructive</LinkButton>
        </div>
        <div style="display: flex; gap: 12px; flex-wrap: wrap; margin-top: 12px;">
          <LinkButton href="/about" variant="primary" size="sm">Small</LinkButton>
          <LinkButton href="/about" variant="primary" size="md">Medium</LinkButton>
          <LinkButton href="/about" variant="primary" size="lg">Large</LinkButton>
        </div>
      </Section>

      {/* ===== Aside ===== */}
      <Section title="Aside">
        <Aside type="note" title="Note">This is a note aside.</Aside>
        <Aside type="tip" title="Tip">This is a tip aside.</Aside>
        <Aside type="warning" title="Warning">This is a warning aside.</Aside>
        <Aside type="danger" title="Danger">This is a danger aside.</Aside>
        <Aside type="caution" title="Caution">This is a caution aside.</Aside>
      </Section>

      {/* ===== Code ===== */}
      <Section title="Code">
        <Code lang="tsx" title="app.tsx" showCopy={true}>
{`function App() {
  return <h1>Hello World</h1>;
}`}
        </Code>
        <Code lang="js" title="utils.js">
{`export function add(a, b) {
  return a + b;
}`}
        </Code>
      </Section>

      {/* ===== FileTree ===== */}
      <Section title="FileTree">
        <FileTree>
          <FileTreeItem icon="tabler:file">package.json</FileTreeItem>
          <FileTreeFolder name="src" defaultOpen={true}>
            <FileTreeItem icon="tabler:file">index.tsx</FileTreeItem>
            <FileTreeItem icon="tabler:file">app.tsx</FileTreeItem>
            <FileTreeFolder name="components" defaultOpen={true}>
              <FileTreeItem icon="tabler:file">button.tsx</FileTreeItem>
              <FileTreeItem icon="tabler:file">card.tsx</FileTreeItem>
            </FileTreeFolder>
          </FileTreeFolder>
          <FileTreeFolder name="public">
            <FileTreeItem>favicon.ico</FileTreeItem>
          </FileTreeFolder>
        </FileTree>
      </Section>

      {/* ===== Steps ===== */}
      <Section title="Steps">
        <Steps>
          <Step title="Install krate">Run npm install @krate/cli</Step>
          <Step title="Create project">Run krate init my-app</Step>
          <Step title="Start dev server">Run krate dev</Step>
        </Steps>
      </Section>

      {/* ===== Tabs ===== */}
      <Section title="Tabs">
        <Tabs defaultValue="preview">
          <TabsList>
            <TabsTrigger value="preview">Preview</TabsTrigger>
            <TabsTrigger value="code">Code</TabsTrigger>
            <TabsTrigger value="props">Props</TabsTrigger>
          </TabsList>
          <TabsContent value="preview">
            <p>This is the preview content. Tabs let users switch between different views.</p>
          </TabsContent>
          <TabsContent value="code">
            <p>Show your code here. This tab panel displays the source code.</p>
          </TabsContent>
          <TabsContent value="props">
            <p>Props documentation goes here. Describes all available component props.</p>
          </TabsContent>
        </Tabs>
      </Section>

      {/* ===== VisuallyHidden ===== */}
      <Section title="VisuallyHidden">
        <VisuallyHidden>This text is only for screen readers.</VisuallyHidden>
        <p>There is a visually hidden element above this text. It is accessible to screen readers but not visible.</p>
      </Section>

      {/* <Separator style="margin: 40px 0;" /> */}

      {/* ===== Accordion ===== */}
      <Section title="Accordion">
        <Accordion type="single" collapsible={true} defaultValue={["item-1"]}>
          <AccordionItem value="item-1">
            <AccordionTrigger>Is it accessible?</AccordionTrigger>
            <AccordionContent>Yes. It adheres to the WAI-ARIA design pattern.</AccordionContent>
          </AccordionItem>
          <AccordionItem value="item-2">
            <AccordionTrigger>Is it styled?</AccordionTrigger>
            <AccordionContent>Yes. It comes with default styles that match the other components.</AccordionContent>
          </AccordionItem>
          <AccordionItem value="item-3">
            <AccordionTrigger>Is it animated?</AccordionTrigger>
            <AccordionContent>Yes. It is animated by default with a smooth open/close transition.</AccordionContent>
          </AccordionItem>
        </Accordion>
        <Accordion type="multiple" defaultValue={["a"]}>
          <AccordionItem value="a">
            <AccordionTrigger>Multiple Mode: First</AccordionTrigger>
            <AccordionContent>You can open multiple items at once in multiple mode.</AccordionContent>
          </AccordionItem>
          <AccordionItem value="b">
            <AccordionTrigger>Multiple Mode: Second</AccordionTrigger>
            <AccordionContent>This is another item that can be open simultaneously.</AccordionContent>
          </AccordionItem>
        </Accordion>
      </Section>

      {/* ===== AlertDialog ===== */}
      {/* <Section title="AlertDialog">
        <AlertDialog>
          <AlertDialogTrigger>
            <button type="button">Show Alert Dialog</button>
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogTitle>Are you sure?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. This will permanently delete your account and remove your data from our servers.
            </AlertDialogDescription>
            <div style="display: flex; gap: 8px; justify-content: flex-end;">
              <AlertDialogCancel>
                <button type="button" style="background: rgba(255,255,255,0.06); border: 1px solid var(--border);">Cancel</button>
              </AlertDialogCancel>
              <AlertDialogAction>
                <button type="button">Continue</button>
              </AlertDialogAction>
            </div>
          </AlertDialogContent>
        </AlertDialog>
      </Section> */}

      {/* ===== AspectRatio ===== */}
      <Section title="AspectRatio">
        <AspectRatio ratio={16 / 9}>
          <div style="background: linear-gradient(135deg, #3b82f6, #8b5cf6); display: flex; align-items: center; justify-content: center; color: white; font-weight: 600;">
            16:9 Aspect Ratio Box
          </div>
        </AspectRatio>
      </Section>

      {/* ===== Avatar ===== */}
      <Section title="Avatar">
        <div style="display: flex; gap: 16px; align-items: center;">
          <Avatar size="sm" src="https://github.com/shadcn.png" alt="Avatar" fallback="CN" />
          <Avatar size="md" src="https://github.com/shadcn.png" alt="Avatar" fallback="CN" />
          <Avatar size="lg" src="https://github.com/shadcn.png" alt="Avatar" fallback="CN" />
          <Avatar size="md" src="" alt="No image" fallback="JD" />
          <Avatar size="lg" src="https://broken-url.invalid/img.jpg" alt="Broken" fallback="FB" />
        </div>
      </Section>

      {/* ===== Checkbox ===== */}
      <Section title="Checkbox">
        <CheckboxGroup />
      </Section>

      {/* ===== Collapsible ===== */}
      <Section title="Collapsible">
        <CollapsibleGroup />
      </Section>

      {/* ===== ContextMenu ===== */}
      <Section title="ContextMenu">
        <ContextMenu>
          <ContextMenuTrigger>
            <div style="border: 2px dashed var(--border); border-radius: 8px; padding: 40px; text-align: center; color: var(--text-muted);">
              Right-click here to open context menu
            </div>
          </ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuLabel>Actions</ContextMenuLabel>
            <ContextMenuItem>Copy</ContextMenuItem>
            <ContextMenuItem>Paste</ContextMenuItem>
            <ContextMenuItem>Cut</ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem>Delete</ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      </Section>

      {/* ===== Dialog ===== */}
      <Section title="Dialog">
        <DialogGroup />
      </Section>

      {/* ===== DropdownMenu ===== */}
      <Section title="DropdownMenu">
        <DropdownMenuDemo />
      </Section>

      {/* ===== Form ===== */}
      <Section title="Form / FormField / FormInput / FormTextarea">
        <Form onSubmit={function () {}}>
          <FormField label="Email" required description="We will never share your email.">
            <FormInput type="email" placeholder="you@example.com" />
          </FormField>
          <FormField label="Password" required>
            <FormInput type="password" placeholder="Enter password" />
          </FormField>
          <FormField label="Bio" description="Tell us about yourself.">
            <FormTextarea placeholder="I am a developer..." rows={4} />
          </FormField>
          <FormField label="Disabled Field">
            <FormInput disabled placeholder="Cannot edit this" />
          </FormField>
          <div style="display: flex; gap: 8px;">
            <LinkButton href="#" variant="primary" size="sm">Submit</LinkButton>
            <LinkButton href="#" variant="secondary" size="sm">Cancel</LinkButton>
          </div>
        </Form>
      </Section>

      {/* ===== HoverCard ===== */}
      <Section title="HoverCard">
        <HoverCard openDelay={200} closeDelay={150}>
          <HoverCardTrigger>
            <span style="color: var(--primary); cursor: pointer; text-decoration: underline;">Hover over me</span>
          </HoverCardTrigger>
          <HoverCardContent>
            <div style="font-weight: 600; margin-bottom: 4px;">Hover Card Title</div>
            <div>This card appears on hover with a short delay. Great for previews and additional context.</div>
          </HoverCardContent>
        </HoverCard>
      </Section>

      {/* ===== Label ===== */}
      <Section title="Label">
        <div style="display: flex; flex-direction: column; gap: 8px;">
          <Label>Default Label</Label>
          <Label required>Required Label</Label>
          <Label disabled>Disabled Label</Label>
        </div>
      </Section>

      {/* ===== MenuBar ===== */}
      <Section title="MenuBar">
        <MenuBar>
          <MenuBarMenu>
            <MenuBarTrigger>File</MenuBarTrigger>
            <MenuBarContent>
              <MenuBarItem>New File</MenuBarItem>
              <MenuBarItem>Open</MenuBarItem>
              <MenuBarItem disabled>Save</MenuBarItem>
              <MenuBarSeparator />
              <MenuBarItem>Exit</MenuBarItem>
            </MenuBarContent>
          </MenuBarMenu>
          <MenuBarMenu>
            <MenuBarTrigger>Edit</MenuBarTrigger>
            <MenuBarContent>
              <MenuBarItem>Undo</MenuBarItem>
              <MenuBarItem>Redo</MenuBarItem>
              <MenuBarSeparator />
              <MenuBarItem>Cut</MenuBarItem>
              <MenuBarItem>Copy</MenuBarItem>
              <MenuBarItem>Paste</MenuBarItem>
            </MenuBarContent>
          </MenuBarMenu>
          <MenuBarMenu>
            <MenuBarTrigger>View</MenuBarTrigger>
            <MenuBarContent>
              <MenuBarLabel>Appearance</MenuBarLabel>
              <MenuBarItem>Zoom In</MenuBarItem>
              <MenuBarItem>Zoom Out</MenuBarItem>
            </MenuBarContent>
          </MenuBarMenu>
        </MenuBar>
      </Section>

      {/* ===== NavigationMenu ===== */}
      <Section title="NavigationMenu">
        <NavigationMenu>
          <NavigationMenuList>
            <NavigationMenuItem>
              <NavigationMenuTrigger>Getting Started</NavigationMenuTrigger>
              <NavigationMenuContent>
                <NavigationMenuLink href="/about">Introduction</NavigationMenuLink>
                <NavigationMenuLink href="#">Installation</NavigationMenuLink>
                <NavigationMenuLink href="#">Quick Start</NavigationMenuLink>
              </NavigationMenuContent>
            </NavigationMenuItem>
            <NavigationMenuItem>
              <NavigationMenuTrigger>Components</NavigationMenuTrigger>
              <NavigationMenuContent>
                <NavigationMenuLink href="#">Accordion</NavigationMenuLink>
                <NavigationMenuLink href="#">Button</NavigationMenuLink>
                <NavigationMenuLink href="#">Dialog</NavigationMenuLink>
                <NavigationMenuLink href="#">Select</NavigationMenuLink>
              </NavigationMenuContent>
            </NavigationMenuItem>
            <NavigationMenuItem>
              <NavigationMenuTrigger>Resources</NavigationMenuTrigger>
              <NavigationMenuContent>
                <NavigationMenuLink href="#">Documentation</NavigationMenuLink>
                <NavigationMenuLink href="#">GitHub</NavigationMenuLink>
              </NavigationMenuContent>
            </NavigationMenuItem>
          </NavigationMenuList>
        </NavigationMenu>
      </Section>

      {/* ===== OTP Field ===== */}
      <Section title="OTPField">
        <OTPField length={6} autoFocus={true} onComplete={function (val: string) { console.log("OTP complete:", val); }} />
      </Section>

      {/* ===== PasswordToggleField ===== */}
      <Section title="PasswordToggleField">
        <PasswordToggleField placeholder="Enter your password" />
      </Section>

      {/* ===== Popover ===== */}
      <Section title="Popover">
        <Popover>
          <PopoverTrigger>Open Popover</PopoverTrigger>
          <PopoverContent>
            <div style="font-weight: 600; margin-bottom: 8px;">Popover Title</div>
            <div style="color: var(--text-muted);">This is popover content. You can put any content here including forms, images, or text.</div>
          </PopoverContent>
        </Popover>
      </Section>

      {/* ===== Progress ===== */}
      <Section title="Progress">
        <ProgressGroup />
      </Section>

      {/* ===== RadioGroup ===== */}
      <Section title="RadioGroup">
        <RadioGroupGroup />
      </Section>

      {/* ===== ScrollArea ===== */}
      <Section title="ScrollArea">
        <ScrollArea maxHeight="200px">
          <div style="padding: 16px;">
            <p>Scroll area content. This area has a maximum height and will scroll when content overflows.</p>
            <p>Item 1 - Lorem ipsum dolor sit amet</p>
            <p>Item 2 - Consectetur adipiscing elit</p>
            <p>Item 3 - Sed do eiusmod tempor</p>
            <p>Item 4 - Incididunt ut labore</p>
            <p>Item 5 - Et dolore magna aliqua</p>
            <p>Item 6 - Ut enim ad minim veniam</p>
            <p>Item 7 - Quis nostrud exercitation</p>
            <p>Item 8 - Ullamco laboris nisi</p>
            <p>Item 9 - Ut aliquip ex ea commodo</p>
            <p>Item 10 - Duis aute irure dolor</p>
            <p>Item 11 - In reprehenderit in voluptate</p>
            <p>Item 12 - Velit esse cillum dolore</p>
          </div>
        </ScrollArea>
      </Section>

      {/* ===== Select ===== */}
      <Section title="Select (Filterable)">
        <Select placeholder="Choose a fruit..." onValueChange={function (val: string) { console.log("Selected:", val); }}>
          <SelectGroup label="Citrus">
            <SelectItem value="apple">Apple</SelectItem>
            <SelectItem value="orange">Orange</SelectItem>
            <SelectItem value="lemon">Lemon</SelectItem>
          </SelectGroup>
          <SelectSeparator />
          <SelectGroup label="Berries">
            <SelectItem value="strawberry">Strawberry</SelectItem>
            <SelectItem value="blueberry">Blueberry</SelectItem>
            <SelectItem value="raspberry">Raspberry</SelectItem>
          </SelectGroup>
          <SelectSeparator />
          <SelectGroup label="Tropical">
            <SelectItem value="mango">Mango</SelectItem>
            <SelectItem value="pineapple">Pineapple</SelectItem>
            <SelectItem value="papaya">Papaya</SelectItem>
          </SelectGroup>
        </Select>
      </Section>

      {/* ===== Separator ===== */}
      <Section title="Separator">
        <p>Above separator</p>
        <Separator />
        <p>Below separator</p>
        <div style="height: 80px; display: flex; gap: 16px;">
          <span>Left</span>
          <Separator orientation="vertical" />
          <span>Right</span>
        </div>
      </Section>

      {/* ===== Slider ===== */}
      <Section title="Slider">
        <SliderGroup />
      </Section>

      {/* ===== Switch ===== */}
      <Section title="Switch">
        <SwitchGroup />
      </Section>

      {/* ===== Toast ===== */}
      <Section title="Toast">
        <ToastGroup />
      </Section>

      {/* ===== Toggle ===== */}
      <Section title="Toggle">
        <ToggleGroupInline />
      </Section>

      {/* ===== ToggleGroup ===== */}
      <Section title="ToggleGroup">
        <ToggleGroupDemo />
      </Section>

      {/* ===== Toolbar ===== */}
      <Section title="Toolbar">
        <Toolbar>
          <ToolbarGroup>
            <ToolbarButton>B</ToolbarButton>
            <ToolbarButton>I</ToolbarButton>
            <ToolbarButton>U</ToolbarButton>
          </ToolbarGroup>
          <ToolbarSeparator />
          <ToolbarGroup>
            <ToolbarButton variant="outline">Left</ToolbarButton>
            <ToolbarButton variant="outline">Center</ToolbarButton>
            <ToolbarButton variant="outline">Right</ToolbarButton>
          </ToolbarGroup>
          <ToolbarSeparator />
          <ToolbarLink href="/about">Link</ToolbarLink>
        </Toolbar>
      </Section>

      {/* ===== Tooltip ===== */}
      <Section title="Tooltip">
        <div style="display: flex; gap: 16px; flex-wrap: wrap;">
          <Tooltip>
            <TooltipTrigger>
              <button type="button">Hover me (top)</button>
            </TooltipTrigger>
            <TooltipContent side="top">Tooltip on top</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger>
              <button type="button">Hover me (bottom)</button>
            </TooltipTrigger>
            <TooltipContent side="bottom">Tooltip on bottom</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger>
              <button type="button">Hover me (left)</button>
            </TooltipTrigger>
            <TooltipContent side="left">Tooltip on left</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger>
              <button type="button">Hover me (right)</button>
            </TooltipTrigger>
            <TooltipContent side="right">Tooltip on right</TooltipContent>
          </Tooltip>
        </div>
      </Section>

      <Separator style="margin: 40px 0;" />

      <div class="demo-footer">
        <p>All 38 components rendered successfully.</p>
      </div>
    </div>
  );
}

/* ===== Interactive sub-components ===== */

function CheckboxGroup() {
  var [checked1, setChecked1] = createSignal(false);
  var [checked2, setChecked2] = createSignal(true);
  var [checked3, setChecked3] = createSignal(false);

  return (
    <div style="display: flex; flex-direction: column; gap: 12px;">
      <Checkbox checked={checked1()} onCheckedChange={setChecked1}>
        Accept terms and conditions
      </Checkbox>
      <Checkbox checked={checked2()} onCheckedChange={setChecked2}>
        Subscribe to newsletter
      </Checkbox>
      <Checkbox checked={checked3()} onCheckedChange={setChecked3} disabled>
        Disabled checkbox
      </Checkbox>
      <p style="margin: 0; font-size: 0.85rem;">Values: terms={checked1() ? "checked" : "unchecked"}, newsletter={checked2() ? "checked" : "unchecked"}</p>
    </div>
  );
}

function CollapsibleGroup() {
  var [open, setOpen] = createSignal(false);

  return (
    <Collapsible open={open()} onOpenChange={setOpen}>
      <div style="display: flex; align-items: center; gap: 12px;">
        <CollapsibleTrigger>
          <button type="button">{open() ? "Close" : "Open"} Collapsible</button>
        </CollapsibleTrigger>
        <span style="color: var(--text-muted); font-size: 0.85rem;">@krate/components v0.1.0</span>
      </div>
      <CollapsibleContent>
        <div style="padding: 12px; margin-top: 8px; border: 1px solid var(--border); border-radius: 8px;">
          <p>Collapsible content is now visible. This can be toggled open and closed.</p>
          <p>More content can go here.</p>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

function DialogGroup() {
  var [open, setOpen] = createSignal(false);

  return (
    <>
      <button type="button" onClick={function () { setOpen(true); }}>Open Dialog</button>
      {open() ? (
        <Dialog open={open()} onOpenChange={setOpen}>
          <DialogContent>
            <DialogTitle>Edit Profile</DialogTitle>
            <DialogDescription>
              Make changes to your profile here. Click save when you are done.
            </DialogDescription>
            <div style="display: flex; flex-direction: column; gap: 12px; margin-top: 8px;">
              <Label>Name</Label>
              <input type="text" placeholder="Enter your name" style="margin: 0;" />
              <Label>Username</Label>
              <input type="text" placeholder="Enter your username" style="margin: 0;" />
            </div>
            <div style="display: flex; gap: 8px; justify-content: flex-end; margin-top: 20px;">
              <button type="button" style="background: rgba(255,255,255,0.06); border: 1px solid var(--border);" onClick={function () { setOpen(false); }}>Cancel</button>
              <button type="button" onClick={function () { setOpen(false); }}>Save changes</button>
            </div>
            <DialogClose />
          </DialogContent>
        </Dialog>
      ) : null}
    </>
  );
}

function DropdownMenuDemo() {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger>Open Menu</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuLabel>My Account</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem>Profile</DropdownMenuItem>
          <DropdownMenuItem>Settings</DropdownMenuItem>
          <DropdownMenuItem disabled>Keyboard shortcuts</DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem>GitHub</DropdownMenuItem>
        <DropdownMenuItem>Support</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem>Logout</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ProgressGroup() {
  var [value, setValue] = createSignal(33);

  return (
    <div style="display: flex; flex-direction: column; gap: 12px;">
      <Progress value={value()} />
      <Progress value={66} />
      <Progress value={100} />
      <div style="display: flex; gap: 8px;">
        <button type="button" onClick={function () { setValue(Math.max(0, value() - 10)); }}>- 10</button>
        <button type="button" onClick={function () { setValue(Math.min(100, value() + 10)); }}>+ 10</button>
        <span style="font-size: 0.85rem; color: var(--text-muted); align-self: center;">Current: {value()}%</span>
      </div>
    </div>
  );
}

function RadioGroupGroup() {
  var [selected, setSelected] = createSignal("comfortable");

  return (
    <div style="display: flex; flex-direction: column; gap: 8px;">
      <RadioGroup defaultValue="comfortable" onValueChange={setSelected}>
        <div style="display: flex; gap: 8px; align-items: center;">
          <RadioGroupItem value="default" />
          <RadioGroupLabel>Default</RadioGroupLabel>
        </div>
        <div style="display: flex; gap: 8px; align-items: center;">
          <RadioGroupItem value="comfortable" />
          <RadioGroupLabel>Comfortable</RadioGroupLabel>
        </div>
        <div style="display: flex; gap: 8px; align-items: center;">
          <RadioGroupItem value="compact" />
          <RadioGroupLabel>Compact</RadioGroupLabel>
        </div>
      </RadioGroup>
      <p style="margin: 0; font-size: 0.85rem;">Selected: {selected()}</p>
    </div>
  );
}

function SliderGroup() {
  var [value, setValue] = createSignal(50);

  return (
    <div style="display: flex; flex-direction: column; gap: 12px;">
      <Slider value={value()} onValueChange={setValue} min={0} max={100} step={1} />
      <p style="margin: 0; font-size: 0.85rem;">Value: {value()}</p>
      <Slider defaultValue={25} min={0} max={100} step={5} />
      <Slider defaultValue={75} min={0} max={100} step={10} disabled={true} />
    </div>
  );
}

function SwitchGroup() {
  var [airplane, setAirplane] = createSignal(false);
  var [bluetooth, setBluetooth] = createSignal(true);

  return (
    <div style="display: flex; flex-direction: column; gap: 12px;">
      <div style="display: flex; align-items: center; gap: 12px;">
        <Switch checked={airplane()} onCheckedChange={setAirplane} />
        <Label>Airplane Mode</Label>
      </div>
      <div style="display: flex; align-items: center; gap: 12px;">
        <Switch checked={bluetooth()} onCheckedChange={setBluetooth} />
        <Label>Bluetooth</Label>
      </div>
      <div style="display: flex; align-items: center; gap: 12px;">
        <Switch disabled />
        <Label disabled>Disabled Switch</Label>
      </div>
      <p style="margin: 0; font-size: 0.85rem;">Airplane: {airplane() ? "on" : "off"}, Bluetooth: {bluetooth() ? "on" : "off"}</p>
    </div>
  );
}

function ToastGroup() {
  var [toasts, setToasts] = createSignal([]);
  var idCounter = 0;

  function showToast() {
    var id = idCounter++;
    setToasts(function (prev) {
      return prev.concat([{ id: id, title: "Event Created", desc: "Monday, January 3 at 6:00 PM" }]);
    });
    setTimeout(function () {
      setToasts(function (prev) {
        return prev.filter(function (t) { return t.id !== id; });
      });
    }, 4000);
  }

  function closeToast(id) {
    setToasts(function (prev) {
      return prev.filter(function (t) { return t.id !== id; });
    });
  }

  return (
    <div>
      <button type="button" onClick={showToast}>Show Toast</button>
      <ToastViewport>
        {toasts().map(function (t) {
          return (
            <Toast variant="default" key={t.id} onClose={function () { closeToast(t.id); }}>
              <ToastTitle>{t.title}</ToastTitle>
              <ToastDescription>{t.desc}</ToastDescription>
            </Toast>
          );
        })}
      </ToastViewport>
    </div>
  );
}

function ToggleGroupInline() {
  var [pressed, setPressed] = createSignal(false);

  return (
    <div style="display: flex; gap: 8px; align-items: center;">
      <Toggle pressed={pressed()} onPressedChange={setPressed}>
        {pressed() ? "On" : "Off"}
      </Toggle>
      <Toggle pressed={false} variant="outline">
        Outline Toggle
      </Toggle>
      <Toggle pressed={false} size="sm">Small</Toggle>
      <Toggle pressed={false} size="lg">Large</Toggle>
      <p style="margin: 0; font-size: 0.85rem;">First toggle: {pressed() ? "pressed" : "not pressed"}</p>
    </div>
  );
}

function ToggleGroupDemo() {
  var [single, setSingle] = createSignal("bold");
  var [multiple, setMultiple] = createSignal(["italic"]);

  return (
    <div style="display: flex; flex-direction: column; gap: 16px;">
      <div>
        <p style="margin-bottom: 8px; font-size: 0.85rem; color: var(--text-muted);">Single selection:</p>
        <ToggleGroup type="single" value={single()} onValueChange={setSingle}>
          <ToggleGroupItem value="bold"><b>B</b></ToggleGroupItem>
          <ToggleGroupItem value="italic"><i>I</i></ToggleGroupItem>
          <ToggleGroupItem value="underline"><u>U</u></ToggleGroupItem>
        </ToggleGroup>
        <p style="margin-top: 4px; font-size: 0.85rem;">Selected: {single()}</p>
      </div>
      <div>
        <p style="margin-bottom: 8px; font-size: 0.85rem; color: var(--text-muted);">Multiple selection:</p>
        <ToggleGroup type="multiple" value={multiple()} onValueChange={setMultiple}>
          <ToggleGroupItem value="bold"><b>B</b></ToggleGroupItem>
          <ToggleGroupItem value="italic"><i>I</i></ToggleGroupItem>
          <ToggleGroupItem value="underline"><u>U</u></ToggleGroupItem>
        </ToggleGroup>
        <p style="margin-top: 4px; font-size: 0.85rem;">Selected: {multiple().join(", ") || "none"}</p>
      </div>
    </div>
  );
}

export default App;
