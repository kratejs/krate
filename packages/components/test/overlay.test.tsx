import { describe, it, expect, beforeEach, vi } from 'vitest';
import { toast } from '../src/toast/toast-api';

// A hand-built scope anchor shaped like the compiler output: a sentinel radio
// (data-ka="x") plus one radio per content value.

describe('toast()', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    vi.useFakeTimers();
  });

  it('renders text as text, never HTML', () => {
    const t = toast({ title: '<img src=x onerror=alert(1)>', duration: 0 });
    const el = document.getElementById(t.id)!;
    expect(el.querySelector('img')).toBeNull();
    expect(el.querySelector('.krate-toast-title')?.textContent).toBe('<img src=x onerror=alert(1)>');
  });
});
