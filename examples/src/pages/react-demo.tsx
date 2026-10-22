import {
  useState,
  useEffect,
  useRef,
  useCallback,
  useMemo,
  useReducer,
  createContext,
  useContext,
  useId,
  useTransition,
  forwardRef,
  memo,
  createRef,
  Children,
  cloneElement,
  Component,
} from 'react';
import * as Dialog from 'radix-ui/dialog';
import * as Tabs from 'radix-ui/tabs';
import * as DropdownMenu from 'radix-ui/dropdown-menu';
import * as Switch from 'radix-ui/switch';
import * as Checkbox from 'radix-ui/checkbox';
import * as Progress from 'radix-ui/progress';
import * as Slider from 'radix-ui/slider';
import * as Accordion from 'radix-ui/accordion';
import * as Select from 'radix-ui/select';
import * as RadioGroup from 'radix-ui/radio-group';
import * as Avatar from 'radix-ui/avatar';
import * as Separator from 'radix-ui/separator';
import * as Collapsible from 'radix-ui/collapsible';
import * as HoverCard from 'radix-ui/hover-card';
import * as Tooltip from 'radix-ui/tooltip';
import * as ScrollArea from 'radix-ui/scroll-area';
import * as ToggleGroup from 'radix-ui/toggle-group';
import * as AspectRatio from 'radix-ui/aspect-ratio';
import * as ContextMenu from 'radix-ui/context-menu';
import * as Menubar from 'radix-ui/menubar';
import * as Popover from 'radix-ui/popover';
import * as Portal from 'radix-ui/portal';
import './react-demo.css';
import Button from '../components/ui/button';
import { Badge } from '../components/ui/shadcn-badge';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';

// ---------------------------------------------------------------------------
// Layout helpers
// ---------------------------------------------------------------------------

function Section(props: { title: string; description?: string; children?: any }) {
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-xl font-semibold text-slate-100">{props.title}</h2>
        {props.description ? <p className="mt-1 text-sm text-slate-400">{props.description}</p> : null}
      </div>
      {props.children}
    </section>
  );
}

function Panel(props: { title: string; note?: string; children?: any }) {
  return (
    <div className="rounded-xl border border-slate-700 bg-slate-900/50 p-5">
      <h3 className="text-sm font-semibold text-slate-100">{props.title}</h3>
      {props.note ? <p className="mt-1 text-xs text-slate-400">{props.note}</p> : null}
      <div className="mt-4">{props.children}</div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// React hooks, context, refs, class components, structural helpers
// ---------------------------------------------------------------------------

function HooksPlayground() {
  const [count, setCount] = useState(0);
  const [reduced, dispatch] = useReducer((state: number, action: string) => {
    return action === 'increment' ? state + 1 : state - 1;
  }, 10);
  const doubled = useMemo(() => count * 2, [count]);
  const reset = useCallback(() => setCount(0), []);

  return (
    <Panel title="Hooks playground" note="useState, useReducer, useMemo, useCallback, and bare-read auto-calling.">
      <div className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" onClick={() => setCount(count + 1)}>count + 1</Button>
          <Button size="sm" variant="outline" onClick={reset}>reset</Button>
          <span className="text-sm text-slate-300">
            count: <strong>{count}</strong> / doubled: <strong>{doubled}</strong>
          </span>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="secondary" onClick={() => dispatch('increment')}>reducer +</Button>
          <Button size="sm" variant="secondary" onClick={() => dispatch('decrement')}>reducer -</Button>
          <span className="text-sm text-slate-300">reducer: <strong>{reduced}</strong></span>
        </div>
        <TimerDisplay />
      </div>
    </Panel>
  );
}

function TimerDisplay() {
  const [seconds, setSeconds] = useState(0);
  const intervalRef = useRef(null);

  useEffect(() => {
    intervalRef.current = setInterval(() => {
      setSeconds((prev: number) => prev + 1);
    }, 1000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, []);

  return (
    <p className="text-sm text-slate-400">
      useEffect timer: <span className="font-mono text-slate-200">{seconds}s</span> (cleaned up on dispose)
    </p>
  );
}

function NameField() {
  const id = useId();
  const [name, setName] = useState('Ada');

  return (
    <div className="space-y-2">
      <Label htmlFor={id}>Display name</Label>
      <input id={id} className="h-9 w-full rounded-md border border-slate-700 bg-slate-950 px-3 text-sm text-slate-100 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-400" value={name} onChange={(event: any) => setName(event.target.value)} />
      <p className="text-sm text-slate-400">Hello, <span className="text-slate-200">{name}</span>.</p>
    </div>
  );
}

const AccentContext = createContext('indigo');

function AccentPanel() {
  const accent = useContext(AccentContext);
  return (
    <div className="rounded-md border border-slate-700 bg-slate-950/60 p-3 text-sm text-slate-300">
      Consumed context value: <strong className="text-slate-100">{accent}</strong>
    </div>
  );
}

function ContextDemo() {
  const [accent, setAccent] = useState('indigo');
  return (
    <div className="space-y-3">
      <AccentContext.Provider value={accent}>
        <AccentPanel />
      </AccentContext.Provider>
      <Button
        size="sm"
        variant="outline"
        onClick={() => setAccent(accent === 'indigo' ? 'emerald' : 'indigo')}
      >
        Toggle accent
      </Button>
    </div>
  );
}

const FancyInput = forwardRef((props: any, ref: any) => (
  <input
    ref={ref}
    placeholder={props.placeholder}
    className="h-9 w-full rounded-md border border-slate-700 bg-slate-950 px-3 text-sm text-slate-100 placeholder:text-slate-500"
  />
));

function ImperativeDemo() {
  const api = useRef(null);
  return (
    <div className="space-y-3">
      <FancyInput ref={api} placeholder="Focused through a forwarded ref" />
      <div className="flex gap-2">
        <Button size="sm" onClick={() => api.current && api.current.focus()}>focus()</Button>
        <Button size="sm" variant="outline" onClick={() => { if (api.current) api.current.value = ''; }}>clear()</Button>
      </div>
    </div>
  );
}

class ClassTimer extends Component {
  state = { seconds: 0 };

  increment = () => {
    this.setState({ seconds: this.state.seconds + 1 });
  };

  tick() {
    this.increment();
  }

  componentDidMount() {
    setTimeout(() => {
      this.tick();
    }, 300);
  }

  componentWillUnmount() {
    console.log('ClassTimer unmounted');
  }

  render() {
    return (
      <div className="rounded-md border border-slate-700 bg-slate-950/60 p-3 text-sm text-slate-300">
        Class component ticked <strong className="text-slate-100">{this.state.seconds}</strong> times
        (state + lifecycle lowered to signals)
        <button className="ml-2 rounded bg-slate-800 px-2 py-0.5 text-xs text-slate-200" onClick={this.tick}>
          tick
        </button>
      </div>
    );
  }
}

const MemoRow = memo((props: { label: string; value: number }) => (
  <li className="flex items-center justify-between rounded border border-slate-800 px-3 py-1 text-sm">
    <span className="text-slate-300">{props.label}</span>
    <span className="font-mono text-slate-400">{props.value}</span>
  </li>
));

function MemoDemo() {
  return (
    <ul className="space-y-1">
      <MemoRow label="memo row one" value={1} />
      <MemoRow label="memo row two" value={2} />
      <MemoRow label="memo row three" value={3} />
    </ul>
  );
}

function ChildrenDemo() {
  useEffect(() => {}, []);
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2">
        {Children.map(
          [
            <button className="rounded bg-slate-800 px-2 py-1 text-xs text-slate-200">Copy</button>,
            <button className="rounded bg-slate-800 px-2 py-1 text-xs text-slate-200">Paste</button>,
            <button className="rounded bg-slate-800 px-2 py-1 text-xs text-slate-200">Cut</button>,
          ],
          (item, index) => (
            <span className="inline-flex items-center gap-1 rounded border border-slate-700 px-2 py-1 text-xs text-slate-300">
              <span className="text-slate-500">{index}</span>
              {item}
            </span>
          )
        )}
      </div>
      <div>
        {cloneElement(
          <span className="rounded bg-slate-800 px-2 py-1 text-xs text-slate-200">cloneElement</span>,
          { className: 'rounded bg-emerald-700 px-2 py-1 text-xs text-white' }
        )}
      </div>
      <p className="text-xs text-slate-400">
        Children.count: {Children.count([<i>a</i>, <i>b</i>, <i>c</i>])} / cloneElement and Children.map rendered above
      </p>
    </div>
  );
}

function CreateRefDemo() {
  const boxRef = createRef();
  return (
    <div className="space-y-2">
      <div ref={boxRef} className="rounded border border-dashed border-slate-600 p-4 text-sm text-slate-300">
        createRef target box
      </div>
      <Button
        size="sm"
        variant="outline"
        onClick={() => {
          const element = boxRef.current;
          alert(element ? 'width: ' + Math.round(element.getBoundingClientRect().width) + 'px' : 'not mounted');
        }}
      >
        Measure width
      </Button>
    </div>
  );
}

const FRUITS = [
  'Apple',
  'Apricot',
  'Banana',
  'Blueberry',
  'Cherry',
  'Cranberry',
  'Date',
  'Fig',
  'Grape',
  'Kiwi',
  'Lemon',
  'Lime',
  'Mango',
  'Melon',
  'Orange',
  'Peach',
  'Pear',
  'Plum',
];

function SearchDemo() {
  const [query, setQuery] = useState('');
  const [pending, startTransition] = useTransition();
  const filtered = useMemo(() => {
    const value = query().toLowerCase();
    return FRUITS.filter((fruit) => fruit.toLowerCase().includes(value));
  }, [query]);

  return (
    <div className="space-y-3">
      <input
        className="h-9 w-full rounded-md border border-slate-700 bg-slate-950 px-3 text-sm text-slate-100 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-400"
        value={query}
        placeholder="Filter fruit..."
        onChange={(event: any) => startTransition(() => setQuery(event.target.value))}
      />
      <p className="text-xs text-slate-400">
        {pending() ? 'Filtering...' : filtered().length + ' matches'} (useTransition + useMemo)
      </p>
      <ul className="grid grid-cols-2 gap-1 text-sm text-slate-300">
        {filtered().map((fruit) => (
          <li key={fruit} className="rounded bg-slate-950/60 px-2 py-1">{fruit}</li>
        ))}
      </ul>
    </div>
  );
}

function OnlineStatus() {
  const [online, setOnline] = useState(true);
  useEffect(() => {
    const update = () => setOnline(navigator.onLine);
    update();
    window.addEventListener('online', update);
    window.addEventListener('offline', update);
    return () => {
      window.removeEventListener('online', update);
      window.removeEventListener('offline', update);
    };
  }, []);

  return (
    <div className="flex items-center gap-2 text-sm text-slate-300">
      <span className={online ? 'h-2 w-2 rounded-full bg-emerald-500' : 'h-2 w-2 rounded-full bg-red-500'} />
      Browser status: {online ? 'online' : 'offline'}
    </div>
  );
}

function ShadcnShowcase() {
  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <div className="rounded-xl border border-slate-700 bg-slate-900/70 p-5">
        <h3 className="text-base font-semibold text-slate-100">Button variants</h3>
        <p className="mt-1 text-sm text-slate-400">cva + cn, folded to static classes at build time.</p>
        <div className="mt-4 flex flex-wrap gap-2">
          <Button>Default</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="outline">Outline</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="destructive">Destructive</Button>
          <Button variant="link">Link</Button>
          <Button size="sm">Small</Button>
          <Button size="lg">Large</Button>
          <Button size="icon">+</Button>
        </div>
      </div>

      <div className="rounded-xl border border-slate-700 bg-slate-900/70 p-5">
        <h3 className="text-base font-semibold text-slate-100">asChild + Slot</h3>
        <p className="mt-1 text-sm text-slate-400">A shadcn Button that renders an anchor instead of a button.</p>
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <Button asChild variant="outline" size="default">
            <a href="/ui-demo">Open UI demo</a>
          </Button>
          <Badge className="border-transparent bg-indigo-600 text-white">Default</Badge>
          <Badge className="border-transparent bg-slate-700 text-slate-100">Secondary</Badge>
          <Badge className="border-transparent bg-emerald-600 text-white">Success</Badge>
          <Badge className="border-transparent bg-amber-500 text-slate-950">Warning</Badge>
          <Badge className="border-transparent bg-red-600 text-white">Destructive</Badge>
          <Badge className="border-slate-600 bg-transparent text-slate-200">Outline</Badge>
        </div>
      </div>

      <div className="rounded-xl border border-slate-700 bg-slate-900/70 p-5 lg:col-span-2">
        <h3 className="text-base font-semibold text-slate-100">Inputs</h3>
        <p className="mt-1 text-sm text-slate-400">Controlled input with useId, htmlFor, and onChange.</p>
        <div className="mt-4">
          <NameField />
        </div>
        <div className="mt-4 flex gap-2">
          <Button size="sm">Save</Button>
          <Button size="sm" variant="ghost">Cancel</Button>
        </div>
      </div>
    </div>
  );
}
// ---------------------------------------------------------------------------
// Radix primitives (each outermost primitive becomes a Preact island)
// ---------------------------------------------------------------------------

function LiveSwitch() {
  const [on, setOn] = useState(true);
  return (
    <div className="flex items-center gap-3">
      <Switch.Root
        checked={on()}
        onCheckedChange={setOn}
        className="relative h-6 w-11 rounded-full bg-slate-700 demo-switch"
      >
        <Switch.Thumb className="block h-5 w-5 translate-x-0.5 rounded-full bg-white transition-transform demo-switch-thumb" />
      </Switch.Root>
      <span className="text-sm text-slate-300">Live switch {on() ? 'on' : 'off'}</span>
    </div>
  );
}

function LiveTooltip() {
  const [open, setOpen] = useState(false);
  return (
    <Tooltip.Provider>
      <Tooltip.Root open={open()} onOpenChange={setOpen}>
        <Tooltip.Trigger
          onPointerEnter={() => setOpen(true)}
          onPointerLeave={() => setOpen(false)}
          onFocus={() => setOpen(true)}
          onBlur={() => setOpen(false)}
          className="rounded-md border border-slate-600 px-3 py-1.5 text-sm text-slate-100"
        >
          Hover me
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            className="z-50 rounded-md bg-slate-800 px-2 py-1 text-xs text-slate-100 shadow"
            sideOffset={6}
          >
            Tooltip content
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  );
}

function RadixShowcase() {
  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
      <Panel title="Dialog" note="Modal focus scope, portal, escape and overlay dismissal.">
        <Dialog.Root>
          <Dialog.Trigger className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm text-white">
            Open dialog
          </Dialog.Trigger>
          <Dialog.Portal>
            <Dialog.Overlay className="fixed inset-0 z-50 bg-black/60" />
            <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[92vw] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-xl border border-slate-700 bg-slate-900 p-6 text-slate-100 shadow-2xl">
              <Dialog.Title className="text-lg font-semibold">Edit profile</Dialog.Title>
              <Dialog.Description className="mt-1 text-sm text-slate-400">
                Make changes, then save or close.
              </Dialog.Description>
              <div className="mt-4 space-y-1">
                <label className="text-sm text-slate-300">Name</label>
                <input
                  className="h-9 w-full rounded-md border border-slate-700 bg-slate-950 px-3 text-sm"
                  defaultValue="Ada Lovelace"
                />
              </div>
              <div className="mt-5 flex justify-end gap-2">
                <Dialog.Close className="rounded-md border border-slate-600 px-3 py-1.5 text-sm">Cancel</Dialog.Close>
                <Dialog.Close className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm text-white">Save</Dialog.Close>
              </div>
            </Dialog.Content>
          </Dialog.Portal>
        </Dialog.Root>
      </Panel>

      <Panel title="Popover" note="Anchored floating content with an arrow.">
        <Popover.Root>
          <Popover.Trigger className="rounded-md border border-slate-600 px-3 py-1.5 text-sm text-slate-100">
            Show popover
          </Popover.Trigger>
          <Popover.Portal>
            <Popover.Content
              className="z-50 w-64 rounded-lg border border-slate-700 bg-slate-900 p-4 text-sm text-slate-300 shadow-xl"
              sideOffset={8}
            >
              <p className="font-medium text-slate-100">Popover title</p>
              <p className="mt-1">Content rendered in a portal and anchored to the trigger.</p>
              <Popover.Arrow className="fill-slate-900" />
            </Popover.Content>
          </Popover.Portal>
        </Popover.Root>
      </Panel>

      <Panel title="Tooltip" note="Controlled Radix Tooltip with page-managed open state.">
        <div className="flex flex-wrap gap-2">
          <LiveTooltip />
        </div>
      </Panel>
      <Panel title="Tabs" note="Keyboard navigation and roving focus.">
        <Tabs.Root defaultValue="overview">
          <Tabs.List className="grid grid-cols-3 gap-1 rounded-lg bg-slate-950 p-1 text-sm">
            <Tabs.Trigger
              value="overview"
              className="rounded-md px-3 py-1.5 demo-tab"
            >
              Overview
            </Tabs.Trigger>
            <Tabs.Trigger
              value="activity"
              className="rounded-md px-3 py-1.5 demo-tab"
            >
              Activity
            </Tabs.Trigger>
            <Tabs.Trigger
              value="settings"
              className="rounded-md px-3 py-1.5 demo-tab"
            >
              Settings
            </Tabs.Trigger>
          </Tabs.List>
          <Tabs.Content value="overview" className="mt-3 text-sm text-slate-300">Overview panel content.</Tabs.Content>
          <Tabs.Content value="activity" className="mt-3 text-sm text-slate-300">Activity panel content.</Tabs.Content>
          <Tabs.Content value="settings" className="mt-3 text-sm text-slate-300">Settings panel content.</Tabs.Content>
        </Tabs.Root>
      </Panel>

      <Panel title="Accordion" note="Single collapsible item with animated content.">
        <Accordion.Root type="single" collapsible defaultValue="shipping" className="rounded-lg border border-slate-800">
          <Accordion.Item value="shipping" className="border-b border-slate-800">
            <Accordion.Header>
              <Accordion.Trigger className="flex w-full items-center justify-between px-3 py-2 text-left text-sm text-slate-200">
                Shipping
                <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
                  <path d="M2 4l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.5" />
                </svg>
              </Accordion.Trigger>
            </Accordion.Header>
            <Accordion.Content className="px-3 pb-3 text-sm text-slate-400">
              Ships in 2-3 business days.
            </Accordion.Content>
          </Accordion.Item>
          <Accordion.Item value="returns">
            <Accordion.Header>
              <Accordion.Trigger className="flex w-full items-center justify-between px-3 py-2 text-left text-sm text-slate-200">
                Returns
                <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
                  <path d="M2 4l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.5" />
                </svg>
              </Accordion.Trigger>
            </Accordion.Header>
            <Accordion.Content className="px-3 pb-3 text-sm text-slate-400">
              Free returns within 30 days.
            </Accordion.Content>
          </Accordion.Item>
        </Accordion.Root>
      </Panel>

      <Panel title="Dropdown menu" note="Items, separator, checkbox item, and radio group.">
        <DropdownMenu.Root>
          <DropdownMenu.Trigger className="rounded-md border border-slate-600 px-3 py-1.5 text-sm text-slate-100">
            Open menu
          </DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content
              className="z-50 min-w-48 rounded-md border border-slate-700 bg-slate-900 p-1 text-sm text-slate-200 shadow-xl"
              sideOffset={6}
            >
              <DropdownMenu.Label className="px-2 py-1.5 text-xs text-slate-500">My account</DropdownMenu.Label>
              <DropdownMenu.Item className="rounded px-2 py-1.5 outline-none demo-menu-item">
                Profile
              </DropdownMenu.Item>
              <DropdownMenu.Item className="rounded px-2 py-1.5 outline-none demo-menu-item">
                Settings
              </DropdownMenu.Item>
              <DropdownMenu.Separator className="my-1 h-px bg-slate-700" />
              <DropdownMenu.CheckboxItem
                defaultChecked
                className="rounded px-2 py-1.5 outline-none demo-menu-item"
              >
                Show status bar
              </DropdownMenu.CheckboxItem>
              <DropdownMenu.Separator className="my-1 h-px bg-slate-700" />
              <DropdownMenu.RadioGroup defaultValue="bottom">
                <DropdownMenu.RadioItem
                  value="top"
                  className="rounded px-2 py-1.5 outline-none demo-menu-item"
                >
                  Top
                </DropdownMenu.RadioItem>
                <DropdownMenu.RadioItem
                  value="bottom"
                  className="rounded px-2 py-1.5 outline-none demo-menu-item"
                >
                  Bottom
                </DropdownMenu.RadioItem>
              </DropdownMenu.RadioGroup>
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>
      </Panel>

      <Panel title="Form controls" note="Switch, checkbox, slider, and progress.">
        <div className="space-y-4">
          <LiveSwitch />

          <div className="flex items-center gap-3">
            <Checkbox.Root
              defaultChecked
              className="flex h-5 w-5 items-center justify-center rounded border border-slate-600 bg-slate-950 demo-checkbox"
            >
              <Checkbox.Indicator>
                <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
                  <path d="M2 6l3 3 5-6" fill="none" stroke="white" strokeWidth="2" />
                </svg>
              </Checkbox.Indicator>
            </Checkbox.Root>
            <span className="text-sm text-slate-300">Checkbox</span>
          </div>

          <div>
            <p className="mb-2 text-sm text-slate-300">Slider</p>
            <Slider.Root defaultValue={[40]} max={100} step={1} className="relative flex h-5 w-full items-center">
              <Slider.Track className="relative h-1 grow rounded-full bg-slate-700">
                <Slider.Range className="absolute h-full rounded-full bg-indigo-500" />
              </Slider.Track>
              <Slider.Thumb className="block h-4 w-4 rounded-full border border-indigo-400 bg-white" aria-label="Value" />
            </Slider.Root>
          </div>

          <div>
            <p className="mb-2 text-sm text-slate-300">Progress</p>
            <Progress.Root value={64} className="relative h-2 w-full overflow-hidden rounded-full bg-slate-800">
              <Progress.Indicator className="h-full bg-indigo-500" style={{ width: '64%' }} />
            </Progress.Root>
          </div>
        </div>
      </Panel>

      <Panel title="Select" note="Portal, popper positioning, typeahead, and item indicator.">
        <Select.Root defaultValue="apple">
          <Select.Trigger
            className="flex h-9 w-full items-center justify-between rounded-md border border-slate-700 bg-slate-950 px-3 text-sm text-slate-100"
            aria-label="Fruit"
          >
            <Select.Value placeholder="Pick a fruit" />
            <Select.Icon>
              <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
                <path d="M2 4l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.5" />
              </svg>
            </Select.Icon>
          </Select.Trigger>
          <Select.Portal>
            <Select.Content
              className="z-50 rounded-md border border-slate-700 bg-slate-900 p-1 text-sm text-slate-200 shadow-xl"
              position="popper"
              sideOffset={4}
            >
              <Select.Viewport>
                <Select.Item value="apple" className="rounded px-2 py-1.5 outline-none demo-menu-item">
                  <Select.ItemText>Apple</Select.ItemText>
                  <Select.ItemIndicator className="ml-2">x</Select.ItemIndicator>
                </Select.Item>
                <Select.Item value="banana" className="rounded px-2 py-1.5 outline-none demo-menu-item">
                  <Select.ItemText>Banana</Select.ItemText>
                  <Select.ItemIndicator className="ml-2">x</Select.ItemIndicator>
                </Select.Item>
                <Select.Item value="cherry" className="rounded px-2 py-1.5 outline-none demo-menu-item">
                  <Select.ItemText>Cherry</Select.ItemText>
                  <Select.ItemIndicator className="ml-2">x</Select.ItemIndicator>
                </Select.Item>
              </Select.Viewport>
            </Select.Content>
          </Select.Portal>
        </Select.Root>
      </Panel>

      <Panel title="Radio group" note="Roving focus with indicator.">
        <RadioGroup.Root defaultValue="comfortable" className="flex flex-col gap-2">
          <RadioGroup.Item value="default" className="flex items-center gap-2 text-sm text-slate-300">
            <span className="flex h-4 w-4 items-center justify-center rounded-full border border-slate-500">
              <RadioGroup.Indicator className="h-2 w-2 rounded-full bg-indigo-500" />
            </span>
            Default
          </RadioGroup.Item>
          <RadioGroup.Item value="comfortable" className="flex items-center gap-2 text-sm text-slate-300">
            <span className="flex h-4 w-4 items-center justify-center rounded-full border border-slate-500">
              <RadioGroup.Indicator className="h-2 w-2 rounded-full bg-indigo-500" />
            </span>
            Comfortable
          </RadioGroup.Item>
          <RadioGroup.Item value="compact" className="flex items-center gap-2 text-sm text-slate-300">
            <span className="flex h-4 w-4 items-center justify-center rounded-full border border-slate-500">
              <RadioGroup.Indicator className="h-2 w-2 rounded-full bg-indigo-500" />
            </span>
            Compact
          </RadioGroup.Item>
        </RadioGroup.Root>
      </Panel>

      <Panel title="Avatar + separator" note="Image with fallback and a semantic separator.">
        <div className="flex items-center gap-3">
          <Avatar.Root className="inline-flex h-10 w-10 overflow-hidden rounded-full border border-slate-700">
            <Avatar.Image src="/hero.png" alt="Krate" className="h-full w-full object-cover" />
            <Avatar.Fallback className="flex h-full w-full items-center justify-center bg-slate-800 text-xs text-slate-300">
              KR
            </Avatar.Fallback>
          </Avatar.Root>
          <div>
            <p className="text-sm text-slate-200">Krate</p>
            <p className="text-xs text-slate-400">Radix avatar</p>
          </div>
        </div>
        <Separator.Root className="my-3 h-px w-full bg-slate-700" />
        <p className="text-sm text-slate-300">Content below the separator.</p>
      </Panel>

      <Panel title="Collapsible" note="Uncontrolled open state.">
        <Collapsible.Root defaultOpen>
          <Collapsible.Trigger className="rounded-md border border-slate-600 px-3 py-1.5 text-sm text-slate-100">
            Toggle details
          </Collapsible.Trigger>
          <Collapsible.Content className="mt-2 text-sm text-slate-400">
            Collapsible content is mounted and removed as the state changes.
          </Collapsible.Content>
        </Collapsible.Root>
      </Panel>

      <Panel title="Hover card" note="Delayed preview in a portal.">
        <HoverCard.Root>
          <HoverCard.Trigger className="text-sm text-indigo-300 underline decoration-dotted">@krate</HoverCard.Trigger>
          <HoverCard.Portal>
            <HoverCard.Content
              className="z-50 w-64 rounded-lg border border-slate-700 bg-slate-900 p-4 text-sm text-slate-300 shadow-xl"
              sideOffset={6}
            >
              <p className="font-medium text-slate-100">Krate</p>
              <p className="mt-1">Signals, SSR, typed API routes, and React islands.</p>
            </HoverCard.Content>
          </HoverCard.Portal>
        </HoverCard.Root>
      </Panel>

      <Panel title="Scroll area" note="Custom scrollbars with viewport sizing.">
        <ScrollArea.Root className="h-40 w-full overflow-hidden rounded-md border border-slate-800">
          <ScrollArea.Viewport className="h-full w-full">
            <div className="space-y-1 p-3 text-sm text-slate-300">
              <p>Row 1</p>
              <p>Row 2</p>
              <p>Row 3</p>
              <p>Row 4</p>
              <p>Row 5</p>
              <p>Row 6</p>
              <p>Row 7</p>
              <p>Row 8</p>
              <p>Row 9</p>
              <p>Row 10</p>
              <p>Row 11</p>
              <p>Row 12</p>
            </div>
          </ScrollArea.Viewport>
          <ScrollArea.Scrollbar orientation="vertical" className="flex w-2 touch-none select-none bg-slate-900 p-0.5">
            <ScrollArea.Thumb className="flex-1 rounded-full bg-slate-600" />
          </ScrollArea.Scrollbar>
          <ScrollArea.Corner />
        </ScrollArea.Root>
      </Panel>

      <Panel title="Toggle group" note="Single selection with roving focus.">
        <ToggleGroup.Root
          type="single"
          defaultValue="center"
          className="inline-flex items-center gap-1 rounded-md border border-slate-700 p-1"
        >
          <ToggleGroup.Item value="left" className="rounded px-2 py-1 text-sm text-slate-300 demo-toggle">
            Left
          </ToggleGroup.Item>
          <ToggleGroup.Item value="center" className="rounded px-2 py-1 text-sm text-slate-300 demo-toggle">
            Center
          </ToggleGroup.Item>
          <ToggleGroup.Item value="right" className="rounded px-2 py-1 text-sm text-slate-300 demo-toggle">
            Right
          </ToggleGroup.Item>
        </ToggleGroup.Root>
      </Panel>

      <Panel title="Aspect ratio" note="Constrained media box.">
        <AspectRatio.Root
          ratio={1.7777}
          className="overflow-hidden rounded-md border border-slate-700 bg-gradient-to-br from-indigo-600/40 to-slate-900"
        >
          <div className="flex h-full w-full items-center justify-center text-sm text-slate-200">16 / 9</div>
        </AspectRatio.Root>
      </Panel>

      <Panel title="Context menu" note="Right-click trigger and portal content.">
        <ContextMenu.Root>
          <ContextMenu.Trigger className="flex h-28 items-center justify-center rounded-md border border-dashed border-slate-600 text-sm text-slate-400">
            Right click inside this box
          </ContextMenu.Trigger>
          <ContextMenu.Portal>
            <ContextMenu.Content className="z-50 min-w-40 rounded-md border border-slate-700 bg-slate-900 p-1 text-sm text-slate-200 shadow-xl">
              <ContextMenu.Item className="rounded px-2 py-1.5 outline-none demo-menu-item">
                Copy
              </ContextMenu.Item>
              <ContextMenu.Item className="rounded px-2 py-1.5 outline-none demo-menu-item">
                Paste
              </ContextMenu.Item>
              <ContextMenu.Separator className="my-1 h-px bg-slate-700" />
              <ContextMenu.Item className="rounded px-2 py-1.5 outline-none demo-menu-item">
                Inspect
              </ContextMenu.Item>
            </ContextMenu.Content>
          </ContextMenu.Portal>
        </ContextMenu.Root>
      </Panel>

      <Panel title="Menubar" note="Menus with nested portals and keyboard access.">
        <Menubar.Root className="flex rounded-md border border-slate-700 bg-slate-950 p-1 text-sm">
          <Menubar.Menu>
            <Menubar.Trigger className="rounded px-2 py-1 text-slate-200 demo-menu-trigger">
              File
            </Menubar.Trigger>
            <Menubar.Portal>
              <Menubar.Content
                className="min-w-40 rounded-md border border-slate-700 bg-slate-900 p-1 text-slate-200 shadow-xl"
                sideOffset={6}
                align="start"
              >
                <Menubar.Item className="rounded px-2 py-1.5 outline-none demo-menu-item">
                  New
                </Menubar.Item>
                <Menubar.Item className="rounded px-2 py-1.5 outline-none demo-menu-item">
                  Open
                </Menubar.Item>
                <Menubar.Separator className="my-1 h-px bg-slate-700" />
                <Menubar.Item className="rounded px-2 py-1.5 outline-none demo-menu-item">
                  Save
                </Menubar.Item>
              </Menubar.Content>
            </Menubar.Portal>
          </Menubar.Menu>
          <Menubar.Menu>
            <Menubar.Trigger className="rounded px-2 py-1 text-slate-200 demo-menu-trigger">
              Edit
            </Menubar.Trigger>
            <Menubar.Portal>
              <Menubar.Content
                className="min-w-40 rounded-md border border-slate-700 bg-slate-900 p-1 text-slate-200 shadow-xl"
                sideOffset={6}
                align="start"
              >
                <Menubar.Item className="rounded px-2 py-1.5 outline-none demo-menu-item">
                  Undo
                </Menubar.Item>
                <Menubar.Item className="rounded px-2 py-1.5 outline-none demo-menu-item">
                  Redo
                </Menubar.Item>
              </Menubar.Content>
            </Menubar.Portal>
          </Menubar.Menu>
        </Menubar.Root>
      </Panel>
    </div>
  );
}

function RadixPortal() {
  return (
    <Portal.Root>
      <div className="fixed bottom-4 right-4 rounded-full bg-emerald-600 px-3 py-1 text-xs font-medium text-white shadow-lg">
        Rendered through Radix Portal
      </div>
    </Portal.Root>
  );
}

export default function ReactDemo() {
  return (
    <div className="react-demo-page mx-auto max-w-6xl space-y-10 px-6 py-10">
      <header className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <Badge className="border-transparent bg-indigo-600 text-white">React compatibility</Badge>
          <Badge className="border-transparent bg-slate-700 text-slate-100">Signals, no VDOM</Badge>
          <Badge className="border-transparent bg-emerald-600 text-white">Preact islands</Badge>
        </div>
        <h1 className="text-3xl font-bold tracking-tight text-slate-100">React demo</h1>
        <p className="max-w-3xl text-sm text-slate-400">
          React source is transpiled to Krate signals and effects at build time. Compiled packages
          such as Radix are bundled into Preact islands. This page exercises hooks, context, refs,
          class components, structural helpers, shadcn-style source components, and a wide set of
          Radix primitives.
        </p>
      </header>

      <Section
        title="State, effects, and context"
        description="React hooks are rewritten to signals; dynamic context values stay reactive across components."
      >
        <div className="grid gap-6 lg:grid-cols-2">
          <HooksPlayground />
          <div className="space-y-6">
            <Panel title="Context" note="createContext + useContext with a changing provider value.">
              <ContextDemo />
            </Panel>
            <Panel title="Refs" note="forwardRef + useRef.">
              <ImperativeDemo />
            </Panel>
          </div>
        </div>
      </Section>

      <Section
        title="Class components, memo, and structural helpers"
        description="Supported class components are lowered; memo is identity; Children/cloneElement/isValidElement fold when static."
      >
        <div className="grid gap-6 lg:grid-cols-2 xl:grid-cols-4">
          <div className="xl:col-span-1"><ClassTimer /></div>
          <div className="xl:col-span-1"><MemoDemo /></div>
          <div className="xl:col-span-1"><ChildrenDemo /></div>
          <div className="xl:col-span-1"><CreateRefDemo /></div>
        </div>
      </Section>

      <Section
        title="Transitions and external stores"
        description="useTransition, useMemo, and browser online status."
      >
        <div className="grid gap-6 lg:grid-cols-2">
          <Panel title="Filter with useTransition" note="Pending state and derived lists driven by signals.">
            <SearchDemo />
          </Panel>
          <Panel title="Browser status" note="Online/offline events update a signal.">
            <OnlineStatus />
          </Panel>
        </div>
      </Section>

      <Section
        title="shadcn-style components"
        description="Copied source patterns: cva, cn, Slot/asChild, rest-prop forwarding, and Tailwind classes."
      >
        <ShadcnShowcase />
      </Section>

      <Section
        title="Radix primitives"
        description="Each outermost Radix primitive is captured as a Preact island and mounted into a placeholder. All demos are uncontrolled so no page callbacks cross the island boundary."
      >
        <RadixShowcase />
      </Section>

      <RadixPortal />
    </div>
  );
}
