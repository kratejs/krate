'use client';
import { useState } from 'react';
import { LayoutGroup, LazyMotion, MotionConfig } from 'motion/react';
import * as m from 'motion/react-m';
import { Tooltip as T } from 'radix-ui';

// The Genesis sidebar: labels that fade and clip when the sidebar collapses to
// a rail, a pill that slides between the active items (layoutId), and on the
// rail a Radix tooltip around each item.
const SPRING = { type: 'spring', stiffness: 420, damping: 36, mass: 0.7 } as const;
const loadFeatures = () => import('./features').then((x) => x.default);

function Label({ show, id, children }: { show: boolean; id: string; children: any }) {
  return <m.span id={id} initial={false} animate={{ opacity: show ? 1 : 0, width: show ? 'auto' : 0 }} style={{ display: 'inline-block', overflow: 'hidden', whiteSpace: 'nowrap', position: 'relative' }}>{children}</m.span>;
}
function Tip({ collapsed, label, children }: { collapsed: boolean; label: string; children: any }) {
  return (
    <T.Root open={collapsed ? undefined : false}>
      <T.Trigger asChild>{children}</T.Trigger>
      <T.Portal><T.Content side="right" sideOffset={10}>{label}<T.Arrow /></T.Content></T.Portal>
    </T.Root>
  );
}
const workspaces = [{ id: 'customer', label: 'Customer' }, { id: 'designer', label: 'Designer' }];
const main = [{ id: 'dashboard', label: 'Dashboard' }, { id: 'orders', label: 'Orders' }];

export default function Sidebar() {
  const [collapsed, setCollapsed] = useState(false);
  const [active, setActive] = useState('customer');
  const item = (w: { id: string; label: string }, layoutId: string) => (
    <Tip key={w.id} collapsed={collapsed} label={w.label}>
      <Link
        href={'#' + w.id}
        id={'item-' + w.id}
        onClick={(e: Event) => { e.preventDefault(); setActive(w.id); setCollapsed(false); }}
        style={{ position: 'relative', display: 'flex', alignItems: 'center', padding: 6 }}
      >
        {active === w.id ? <m.span layoutId={layoutId} data-pill={w.id} style={{ position: 'absolute', inset: 0, background: '#ddd' }} /> : null}
        <i style={{ position: 'relative' }}>*</i>
        <Label show={!collapsed} id={'label-' + w.id}>{w.label}</Label>
      </Link>
    </Tip>
  );
  return (
    <LazyMotion features={loadFeatures} strict>
      <MotionConfig reducedMotion="user" transition={SPRING}>
        <T.Provider delayDuration={120}>
          <m.aside id="sidebar" initial={false} animate={{ width: collapsed ? 72 : 240 }} style={{ overflowX: 'hidden', display: 'flex', flexDirection: 'column' }}>
            <button id="toggle" onClick={() => setCollapsed((c) => !c)}>{collapsed ? 'expand' : 'collapse'}</button>
            <LayoutGroup id="sidebar">
              <nav aria-label="Workspace" style={{ display: 'flex', flexDirection: collapsed ? 'column' : 'row', gap: 4, padding: 4 }}>
                {workspaces.map((w) => item(w, 'sidebar-workspace-pill'))}
              </nav>
              <nav aria-label="Main">{main.map((w) => item(w, 'sidebar-pill'))}</nav>
            </LayoutGroup>
          </m.aside>
        </T.Provider>
      </MotionConfig>
    </LazyMotion>
  );
}
