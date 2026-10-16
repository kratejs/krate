import { describe, it, expect } from 'vitest';
import { Badge } from '../src/badge/badge';
import { Skeleton } from '../src/skeleton/skeleton';
import { Alert } from '../src/alert/alert';
import { Button } from '../src/button/button';

describe('Badge', () => {
  it('renders variant and size classes', () => {
    const el = Badge({ children: 'Hi', variant: 'success', size: 'sm' }) as HTMLElement;
    expect(el.tagName).toBe('SPAN');
    expect(el.className).toContain('krate-badge-success');
    expect(el.className).toContain('krate-badge-sm');
    expect(el.textContent).toBe('Hi');
  });

  it('defaults to default/md', () => {
    const el = Badge({ children: 'x' }) as HTMLElement;
    expect(el.className).toContain('krate-badge-default');
    expect(el.className).toContain('krate-badge-md');
  });
});

describe('Skeleton', () => {
  it('is a hidden static placeholder', () => {
    const el = Skeleton({ class: 'w-4' }) as HTMLElement;
    expect(el.tagName).toBe('DIV');
    expect(el.className).toContain('krate-skeleton');
    expect(el.getAttribute('aria-hidden')).toBe('true');
  });
});

describe('Alert', () => {
  it('applies the variant class and role', () => {
    const el = Alert({ children: 'body', title: 'T', variant: 'destructive' }) as HTMLElement;
    expect(el.className).toContain('krate-alert-destructive');
    expect(el.getAttribute('role')).toBe('alert');
    expect(el.textContent).toContain('T');
    expect(el.textContent).toContain('body');
  });
});

describe('Button', () => {
  it('renders a button with children', () => {
    const el = Button({ children: 'Go' }) as HTMLElement;
    expect(el.tagName).toBe('BUTTON');
    expect(el.textContent).toBe('Go');
  });
});
