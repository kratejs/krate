import { describe, it, expect } from 'vitest';
import { OTPField } from '../src/otp-field/otp-field';

function mount(props: Record<string, any> = {}) {
  const el = OTPField({ length: 4, ...props }) as HTMLElement;
  document.body.appendChild(el);
  const boxes = () => Array.from(el.querySelectorAll('.krate-otp-field-input')) as HTMLInputElement[];
  return { el, boxes, value: () => boxes().map((b) => b.value).join('') };
}

function type(input: HTMLInputElement, ch: string) {
  input.value = ch;
  input.dispatchEvent(new InputEvent('input', { data: ch, inputType: 'insertText', bubbles: true }));
}

function key(input: HTMLInputElement, k: string) {
  const ev = new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true });
  input.dispatchEvent(ev);
  return ev;
}

describe('OTPField', () => {
  it('shows typed digits and moves to the next box', () => {
    const { boxes, value } = mount();
    const b = boxes();
    b[0].focus();
    type(b[0], '1');
    expect(value()).toBe('1');
    expect(document.activeElement).toBe(b[1]);
  });

  it('Backspace removes digits from the DOM', () => {
    const { boxes, value } = mount();
    const b = boxes();
    type(b[0], '1');
    type(b[1], '2');
    // On the empty third box, Backspace steps back and clears the previous digit.
    b[2].focus();
    const ev = key(b[2], 'Backspace');
    expect(ev.defaultPrevented).toBe(true);
    expect(value()).toBe('1');
    expect(document.activeElement).toBe(b[1]);
    // On a filled box the browser's own deletion is left alone.
    b[0].focus();
    expect(key(b[0], 'Backspace').defaultPrevented).toBe(false);
  });

  it('paste fills the boxes and completes', () => {
    let done = '';
    const { el, value } = mount({ onComplete: (v: string) => (done = v) });
    const data = new DataTransfer();
    data.setData('text', '98-76');
    el.querySelector('input')!.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
    expect(value()).toBe('9876');
    expect(done).toBe('9876');
  });

  it('spreads an autofilled code across the boxes', () => {
    const { boxes, value } = mount();
    const first = boxes()[0];
    first.value = '5678';
    first.dispatchEvent(new Event('input', { bubbles: true }));
    expect(value()).toBe('5678');
  });

  it('ignores characters that are not digits or letters and keeps the old digit', () => {
    const { boxes, value } = mount();
    const b = boxes();
    type(b[0], '1');
    b[0].focus();
    type(b[0], '!');
    expect(value()).toBe('1');
  });

  it('starts from the value prop and submits under name', () => {
    const { el, value } = mount({ value: '12', name: 'otp' });
    expect(value()).toBe('12');
    const hidden = el.querySelector('input[type=hidden]') as HTMLInputElement;
    expect(hidden.name).toBe('otp');
    expect(hidden.value).toBe('12');
    expect(el.querySelector('input')!.getAttribute('autocomplete')).toBe('one-time-code');
  });

  it('keeps the hidden form value in step with the boxes', () => {
    const { boxes, el } = mount({ name: 'otp' });
    type(boxes()[0], '7');
    expect((el.querySelector('input[type=hidden]') as HTMLInputElement).value).toBe('7');
  });
});
