import {
  createCSSChoice,
  createCSSToggle,
  createCSSFlags,
  createCSSGroup,
  createCSSRange,
  createCSSStack,
} from '@krate/runtime';

function Section(props: { title: string; note?: string; children?: any }) {
  return (
    <section class="sig-section">
      <h2 class="sig-title">{props.title}</h2>
      {props.note ? <p class="sig-note">{props.note}</p> : null}
      <div class="sig-body">{props.children}</div>
    </section>
  );
}

/* createCSSToggle: show/hide, with live text */
function ToggleDemo() {
  const [on, setOn] = createCSSToggle(false);

  return (
    <div class="sig-row">
      <button class="sig-btn" onClick={() => setOn(!on())}>
        Toggle panel
      </button>
      <p class="sig-readout">
        Toggle state: {on()} (live text, zero JS)
      </p>
      <div class="sig-panel" showIf={on()}>
        Toggle is <strong>on</strong> — shown with zero JavaScript.
      </div>
      <div class="sig-panel muted" showIf={!on()}>
        Toggle is <strong>off</strong>.
      </div>
    </div>
  );
}

/* createCSSToggle as a switch (native role=switch) */
function SwitchDemo() {
  const [dark, setDark] = createCSSToggle(false, { as: 'switch', label: 'Dark mode' });

  return (
    <div class="sig-row">
      <button class="sig-btn" onClick={() => setDark(!dark())}>
        Toggle dark mode
      </button>
      <div class="sig-panel" showIf={dark()}>
        Dark mode is enabled (aria role=switch).
      </div>
      <div class="sig-panel muted" showIf={!dark()}>
        Dark mode is disabled.
      </div>
    </div>
  );
}

/* createCSSChoice: tabs, segmented controls */
function ChoiceDemo() {
  const [view, setView] = createCSSChoice('grid', ['grid', 'list', 'table']);

  return (
    <div class="sig-row">
      <div class="sig-seg">
        <button class="sig-seg-btn" onClick={() => setView('grid')}>Grid</button>
        <button class="sig-seg-btn" onClick={() => setView('list')}>List</button>
        <button class="sig-seg-btn" onClick={() => setView('table')}>Table</button>
      </div>
      <p class="sig-readout">Selected view: {view()}</p>
      <div class="sig-panel" showIf={view() === 'grid'}>Grid view selected.</div>
      <div class="sig-panel" showIf={view() === 'list'}>List view selected.</div>
      <div class="sig-panel" showIf={view() === 'table'}>Table view selected.</div>
    </div>
  );
}

/* createCSSChoice with an explicit tabs ARIA role */
function TabsAriaDemo() {
  const [tab, setTab] = createCSSChoice('overview', { as: 'tabs', label: 'Project sections' });

  return (
    <div class="sig-row">
      <div class="sig-seg">
        <button class="sig-seg-btn" onClick={() => setTab('overview')}>Overview</button>
        <button class="sig-seg-btn" onClick={() => setTab('activity')}>Activity</button>
        <button class="sig-seg-btn" onClick={() => setTab('settings')}>Settings</button>
      </div>
      <div class="sig-panel" showIf={tab() === 'overview'}>Overview panel (role=tabpanel).</div>
      <div class="sig-panel" showIf={tab() === 'activity'}>Activity panel.</div>
      <div class="sig-panel" showIf={tab() === 'settings'}>Settings panel.</div>
    </div>
  );
}

/* createCSSFlags: filter chips, column toggles */
function FlagsDemo() {
  const [filters, setFilter] = createCSSFlags(['vegan', 'glutenFree', 'spicy']);

  return (
    <div class="sig-row">
      <div class="sig-chips">
        <button class="sig-chip" onClick={() => setFilter('vegan', !filters.vegan())}>Vegan</button>
        <button class="sig-chip" onClick={() => setFilter('glutenFree', !filters.glutenFree())}>Gluten-free</button>
        <button class="sig-chip" onClick={() => setFilter('spicy', !filters.spicy())}>Spicy</button>
      </div>
      <div class="sig-panel" showIf={filters.vegan()}>Vegan filter is on.</div>
      <div class="sig-panel" showIf={!filters.glutenFree()}>Contains gluten.</div>
      <div class="sig-panel" showIf={filters.vegan() && filters.spicy()}>
        Vegan and spicy (compound condition).
      </div>
    </div>
  );
}

/* compound showIf across scopes */
function CompoundDemo() {
  const [plan, setPlan] = createCSSChoice('free', ['free', 'pro']);
  const [admin, setAdmin] = createCSSToggle(false);

  return (
    <div class="sig-row">
      <button class="sig-btn" onClick={() => setPlan('free')}>Free</button>
      <button class="sig-btn" onClick={() => setPlan('pro')}>Pro</button>
      <button class="sig-btn" onClick={() => setAdmin(!admin())}>Toggle admin</button>
      <div class="sig-panel" showIf={plan() === 'pro' && admin()}>
        Pro + admin only (AND across scopes).
      </div>
      <div class="sig-panel" showIf={plan() === 'free' || !admin()}>
        Free, or admin is off (OR).
      </div>
      <div class="sig-panel" showIf={!(plan() === 'pro' || admin())}>
        Neither pro nor admin (De Morgan).
      </div>
    </div>
  );
}

/* createCSSGroup: single-open accordion (radio + null sentinel) */
function GroupDemo() {
  const [open, setOpen] = createCSSGroup(null, { as: 'accordion', label: 'FAQ' });

  return (
    <div class="sig-row">
      <button class="sig-acc" onClick={() => setOpen('q1')}>What is Krate?</button>
      <div class="sig-panel" showIf={open() === 'q1'}>
        A Go-native static site generator with signal-based reactivity.
      </div>

      <button class="sig-acc" onClick={() => setOpen('q2')}>Are the CSS primitives zero-JS?</button>
      <div class="sig-panel" showIf={open() === 'q2'}>
        Yes — they compile to hidden inputs and :has() CSS. Only stateful ARIA
        roles inject a tiny synchroniser.
      </div>

      <button class="sig-acc" onClick={() => setOpen('q3')}>How do I close it?</button>
      <div class="sig-panel" showIf={open() === 'q3'}>
        Select the open item again, or use a close trigger below.
      </div>

      <button class="sig-btn" onClick={() => setOpen(null)}>Close all</button>
    </div>
  );
}

/* createCSSGroup as a dropdown menu role */
function MenuDemo() {
  const [open, setOpen] = createCSSGroup(null, { as: 'menu', label: 'Actions' });

  return (
    <div class="sig-row">
      <button class="sig-btn" onClick={() => setOpen('menu')}>Open menu</button>
      <div class="sig-panel" showIf={open() === 'menu'}>
        <div class="sig-menu-item">Profile</div>
        <div class="sig-menu-item">Settings</div>
        <div class="sig-menu-item">Sign out</div>
      </div>
      <button class="sig-btn" onClick={() => setOpen(null)}>Close menu</button>
    </div>
  );
}

/* createCSSRange: stepper, progress, stepped slider */
function RangeDemo() {
  const [level, setLevel] = createCSSRange(2, { min: 0, max: 5, step: 1 });

  return (
    <div class="sig-row">
      <div class="sig-stepper">
        <button class="sig-btn" onClick={() => setLevel(level() - 1)}>−</button>
        <button class="sig-btn" onClick={() => setLevel(level() + 1)}>+</button>
      </div>
      <div class="sig-progress">
        <div class="krc-fill"></div>
      </div>
      <div class="sig-panel" showIf={level() === 0}>At minimum (0).</div>
      <div class="sig-panel" showIf={level() === 3}>Halfway (3).</div>
      <div class="sig-panel" showIf={level() === 5}>At maximum (5).</div>
    </div>
  );
}

/* createCSSStack: drill-down navigation */
function StackDemo() {
  const [stack, { push, pop, clear }] = createCSSStack(['home']);

  return (
    <div class="sig-row">
      <button class="sig-btn" onClick={() => push('settings')}>Open settings</button>

      <div class="sig-panel" showIf={stack.top() === 'settings'}>
        <strong>Settings</strong>
        <div class="sig-stack-actions">
          <button class="sig-btn" onClick={() => pop()}>Back</button>
          <button class="sig-btn" onClick={() => push('notifications')}>Notifications</button>
        </div>
      </div>

      <div class="sig-panel" showIf={stack.top() === 'notifications'}>
        <strong>Notifications</strong>
        <div class="sig-stack-actions">
          <button class="sig-btn" onClick={() => pop()}>Back</button>
          <button class="sig-btn" onClick={() => push('sound')}>Sound</button>
        </div>
      </div>

      <div class="sig-panel" showIf={stack.top() === 'sound'}>
        <strong>Sound</strong>
        <div class="sig-stack-actions">
          <button class="sig-btn" onClick={() => pop()}>Back</button>
          <button class="sig-btn" onClick={() => clear()}>Home</button>
        </div>
      </div>

      <button class="sig-btn ghost" onClick={() => clear()}>Reset to home</button>
    </div>
  );
}

export default function CssSignalsPage() {
  return (
    <div class="sig-page">
      <h1>CSS Signal primitives</h1>
      <p class="sig-note">
        Every zero-JS CSS primitive compiled by Krate, on one page: hidden inputs,
        <code>:has()</code> CSS, and a tiny ARIA synchroniser only where state
        cannot be inferred natively.
      </p>

      <Section title="createCSSToggle" note="Single checkbox — show/hide.">
        <ToggleDemo />
      </Section>

      <Section title="createCSSToggle { as: 'switch' }" note="Native switch semantics.">
        <SwitchDemo />
      </Section>

      <Section title="createCSSChoice" note="Radio group — tabs and segmented controls.">
        <ChoiceDemo />
      </Section>

      <Section title="createCSSChoice { as: 'tabs' }" note="Tablist ARIA + synchroniser.">
        <TabsAriaDemo />
      </Section>

      <Section title="createCSSFlags" note="Independent checkboxes — filter chips.">
        <FlagsDemo />
      </Section>

      <Section title="Compound showIf" note="&& / || / ! across scopes.">
        <CompoundDemo />
      </Section>

      <Section title="createCSSGroup { as: 'accordion' }" note="Optional radio group with a null sentinel.">
        <GroupDemo />
      </Section>

      <Section title="createCSSGroup { as: 'menu' }" note="Dropdown menu role.">
        <MenuDemo />
      </Section>

      <Section title="createCSSRange" note="Discrete range — stepper, progress, slider.">
        <RangeDemo />
      </Section>

      <Section title="createCSSStack" note="Declared drill-down navigation tree.">
        <StackDemo />
      </Section>
    </div>
  );
}
