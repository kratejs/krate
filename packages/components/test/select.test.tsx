import { describe, it, expect } from 'vitest';
import { NativeSelect } from '../src/select/select';

const fruit = [
  { value: 'apple', label: 'Apple' },
  { value: 'orange', label: 'Orange' },
];

describe('NativeSelect', () => {
  it('selects the placeholder when there is no default value', () => {
    const el = NativeSelect({ placeholder: 'Choose...', options: fruit }) as HTMLSelectElement;
    const first = el.querySelectorAll('option')[0] as HTMLOptionElement;
    expect(first.textContent).toBe('Choose...');
    expect(first.selected || first.hasAttribute('selected')).toBe(true);
  });

  it('leaves the placeholder unselected when a default value is given', () => {
    const el = NativeSelect({ placeholder: 'Choose...', options: fruit, defaultValue: 'orange' }) as HTMLSelectElement;
    const first = el.querySelectorAll('option')[0] as HTMLOptionElement;
    expect(first.selected || first.hasAttribute('selected')).toBe(false);
  });
});
