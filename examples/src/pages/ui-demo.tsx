import {
  Button,
  Badge,
  Input,
  Textarea,
  Skeleton,
  Alert,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  TableCaption,
  Breadcrumb,
  BreadcrumbList,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbPage,
  BreadcrumbSeparator,
  Pagination,
  Sheet,
  SheetTrigger,
  SheetContent,
  SheetTitle,
  SheetDescription,
  SheetClose,
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
          <BreadcrumbList>
            <BreadcrumbItem><BreadcrumbLink href="/">Home</BreadcrumbLink></BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem><BreadcrumbLink href="/docs">Docs</BreadcrumbLink></BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem><BreadcrumbPage>Components</BreadcrumbPage></BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
      </section>

      <section>
        <h2>Table</h2>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow>
              <TableCell>Ada</TableCell>
              <TableCell>Engineer</TableCell>
              <TableCell><Badge variant="success">Active</Badge></TableCell>
            </TableRow>
            <TableRow>
              <TableCell>Grace</TableCell>
              <TableCell>Compiler</TableCell>
              <TableCell><Badge variant="warning">Away</Badge></TableCell>
            </TableRow>
          </TableBody>
          <TableCaption>Team members</TableCaption>
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
          <SheetTrigger>Open settings</SheetTrigger>
          <SheetContent side="right">
            <SheetTitle>Settings</SheetTitle>
            <SheetDescription>Adjust your preferences.</SheetDescription>
            <SheetClose />
          </SheetContent>
        </Sheet>
      </section>

      <section>
        <h2>Command</h2>
        <Command items={commandItems} placeholder="Search commands..." />
      </section>
    </div>
  );
}
