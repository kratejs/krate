import { describe, it, expect, beforeEach, vi } from 'vitest';
import { krateOn, krateControl, krateEventName, krateEventRoot } from './events';
import { createSignal } from './signal';
import { h } from './dom';

const tick = () => new Promise((r) => setTimeout(r, 0));

function setup(html: string): HTMLElement {
  document.body.innerHTML = '<div id="root">' + html + '</div>';
  return document.getElementById('root')!;
}

function $(sel: string): HTMLElement {
  return document.querySelector(sel) as HTMLElement;
}

function fire(el: EventTarget, type: string, init: EventInit = { bubbles: true, cancelable: true }): Event {
  const e = new Event(type, init);
  el.dispatchEvent(e);
  return e;
}

describe('krateEventName', () => {
  it('splits capture variants but keeps pointer-capture event names', () => {
    expect(krateEventName('onClick')).toEqual(['click', false]);
    expect(krateEventName('onClickCapture')).toEqual(['click', true]);
    expect(krateEventName('onGotPointerCapture')).toEqual(['gotpointercapture', false]);
    expect(krateEventName('onLostPointerCaptureCapture')).toEqual(['lostpointercapture', true]);
  });
});

describe('delegated events', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  it('runs capture handlers top-down, then bubble handlers bottom-up', () => {
    setup('<div id="a"><div id="b"><button id="c">x</button></div></div>');
    const log: string[] = [];
    for (const id of ['a', 'b', 'c']) {
      krateOn($('#' + id), 'onClickCapture', () => log.push(id + ':capture'));
      krateOn($('#' + id), 'onClick', () => log.push(id + ':bubble'));
    }
    $('#c').click();
    expect(log).toEqual(['a:capture', 'b:capture', 'c:capture', 'c:bubble', 'b:bubble', 'a:bubble']);
  });

  it('sets currentTarget to the element whose handler runs, and clears it after', () => {
    setup('<div id="a"><button id="c">x</button></div>');
    const seen: Array<[string, string, string]> = [];
    let saved: any;
    krateOn($('#a'), 'onClick', (e) => {
      seen.push([(e.currentTarget as Element).id, (e.target as Element).id, e.type]);
      saved = e;
    });
    krateOn($('#c'), 'onClick', (e) => seen.push([(e.currentTarget as Element).id, (e.target as Element).id, e.type]));
    $('#c').click();
    expect(seen).toEqual([
      ['c', 'c', 'click'],
      ['a', 'c', 'click'],
    ]);
    expect(saved.currentTarget).toBe(null);
  });

  it('exposes the synthetic event surface', () => {
    setup('<input id="i">');
    let got: any;
    krateOn($('#i'), 'onKeyDown', (e) => {
      got = e;
      e.persist();
    });
    const native = new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', shiftKey: true, bubbles: true, cancelable: true });
    $('#i').dispatchEvent(native);
    expect(got.nativeEvent).toBe(native);
    expect(got.key).toBe('Enter');
    expect(got.code).toBe('Enter');
    expect(got.shiftKey).toBe(true);
    expect(got.bubbles).toBe(true);
    expect(got.cancelable).toBe(true);
    expect(typeof got.timeStamp).toBe('number');
    expect(got.isDefaultPrevented()).toBe(false);
    expect(got.isPropagationStopped()).toBe(false);
    expect(got.isPersistent()).toBe(true);
    expect(got.getModifierState('Shift')).toBe(native.getModifierState('Shift'));
  });

  it('preventDefault reaches the native event', () => {
    setup('<a id="l" href="#x">x</a>');
    let synthetic: any;
    krateOn($('#l'), 'onClick', (e) => {
      e.preventDefault();
      synthetic = e;
    });
    const native = fire($('#l'), 'click');
    expect(native.defaultPrevented).toBe(true);
    expect(synthetic.defaultPrevented).toBe(true);
    expect(synthetic.isDefaultPrevented()).toBe(true);
  });

  it('stopPropagation stops React ancestors and native listeners above the root', () => {
    setup('<div id="a"><button id="c">x</button></div>');
    const log: string[] = [];
    krateOn($('#a'), 'onClick', () => log.push('a'));
    krateOn($('#c'), 'onClick', (e) => {
      log.push('c');
      e.stopPropagation();
      expect(e.isPropagationStopped()).toBe(true);
    });
    const onDoc = () => log.push('document');
    document.addEventListener('click', onDoc);
    $('#c').click();
    document.removeEventListener('click', onDoc);
    expect(log).toEqual(['c']);
  });

  it('stopPropagation in a capture handler prevents bubble handlers', () => {
    setup('<div id="a"><button id="c">x</button></div>');
    const log: string[] = [];
    krateOn($('#a'), 'onClickCapture', (e) => {
      log.push('a:capture');
      e.stopPropagation();
    });
    krateOn($('#c'), 'onClick', () => log.push('c'));
    $('#c').click();
    expect(log).toEqual(['a:capture']);
  });

  it('interleaves with native listeners like React 17+ root delegation', () => {
    setup('<div id="a"><button id="c">x</button></div>');
    const log: string[] = [];
    krateOn($('#a'), 'onClick', () => log.push('react a'));
    krateOn($('#c'), 'onClick', () => log.push('react c'));
    krateOn($('#c'), 'onClickCapture', () => log.push('react c capture'));
    const c = $('#c');
    const nativeC = () => log.push('native c');
    const nativeCap = () => log.push('native c capture');
    const nativeDoc = () => log.push('native document');
    const nativeDocCap = () => log.push('native document capture');
    c.addEventListener('click', nativeC);
    c.addEventListener('click', nativeCap, true);
    document.addEventListener('click', nativeDoc);
    document.addEventListener('click', nativeDocCap, true);
    c.click();
    document.removeEventListener('click', nativeDoc);
    document.removeEventListener('click', nativeDocCap, true);
    expect(log).toEqual([
      'native document capture',
      'react c capture',
      'native c capture',
      'native c',
      'react c',
      'react a',
      'native document',
    ]);
  });

  it('a native stopPropagation inside the root hides the event from React', () => {
    setup('<div id="a"><button id="c">x</button></div>');
    const log: string[] = [];
    krateOn($('#a'), 'onClick', () => log.push('react a'));
    $('#c').addEventListener('click', (e) => e.stopPropagation());
    $('#c').click();
    expect(log).toEqual([]);
  });

  it('maps onDoubleClick, onFocus and onBlur', () => {
    setup('<div id="w"><input id="i"></div>');
    const log: string[] = [];
    krateOn($('#w'), 'onDoubleClick', (e) => log.push('dbl:' + e.type));
    krateOn($('#w'), 'onFocus', (e) => log.push('focus:' + e.type));
    krateOn($('#w'), 'onBlur', (e) => log.push('blur:' + e.type));
    fire($('#i'), 'dblclick');
    fire($('#i'), 'focusin');
    fire($('#i'), 'focusout');
    expect(log).toEqual(['dbl:dblclick', 'focus:focus', 'blur:blur']);
  });

  it('attaches non-bubbling events to the element', () => {
    setup('<div id="p"><div id="c"></div></div>');
    const log: string[] = [];
    krateOn($('#p'), 'onScroll', () => log.push('p scroll'));
    krateOn($('#c'), 'onScroll', (e) => log.push('c scroll ' + (e.currentTarget as Element).id));
    krateOn($('#p'), 'onMouseEnter', (e) => log.push('p enter ' + e.type));
    krateOn($('#c'), 'onMouseLeave', () => log.push('c leave'));
    fire($('#c'), 'scroll', { bubbles: false });
    fire($('#p'), 'mouseenter', { bubbles: false });
    fire($('#c'), 'mouseleave', { bubbles: false });
    fire($('#c'), 'mouseenter', { bubbles: false });
    expect(log).toEqual(['c scroll c', 'p enter mouseenter', 'c leave']);
  });

  it('dispatches media and composition events', () => {
    setup('<div id="w"><video id="v"></video><input id="i"></div>');
    const log: string[] = [];
    krateOn($('#v'), 'onPlay', () => log.push('play'));
    krateOn($('#w'), 'onPlay', () => log.push('wrapper play'));
    krateOn($('#w'), 'onCompositionStart', (e) => log.push('start ' + e.data));
    krateOn($('#w'), 'onCompositionEnd', (e) => log.push('end ' + e.data));
    krateOn($('#w'), 'onBeforeInput', (e) => log.push('before ' + e.data));
    fire($('#v'), 'play', { bubbles: false });
    // happy-dom's CompositionEvent has no data; use plain events carrying it.
    const comp = (type: string, data: string) => Object.assign(new Event(type, { bubbles: true }), { data });
    $('#i').dispatchEvent(comp('compositionstart', ''));
    $('#i').dispatchEvent(comp('compositionend', 'ka'));
    $('#i').dispatchEvent(new InputEvent('beforeinput', { bubbles: true, data: 'x', inputType: 'insertText' }));
    $('#i').dispatchEvent(new InputEvent('beforeinput', { bubbles: true, inputType: 'deleteContentBackward' }));
    expect(log).toEqual(['play', 'start ', 'end ka', 'before ka', 'before x']);
  });

  it('dispatches onSubmit and onReset', () => {
    setup('<form id="f"><button id="b">go</button></form>');
    const log: string[] = [];
    krateOn($('#f'), 'onSubmit', (e) => {
      e.preventDefault();
      log.push('submit');
    });
    krateOn($('#f'), 'onReset', () => log.push('reset'));
    const s = fire($('#f'), 'submit');
    fire($('#f'), 'reset');
    expect(s.defaultPrevented).toBe(true);
    expect(log).toEqual(['submit', 'reset']);
  });

  it('replaces and removes handlers', () => {
    setup('<button id="c">x</button>');
    const log: string[] = [];
    krateOn($('#c'), 'onClick', () => log.push('one'));
    krateOn($('#c'), 'onClick', () => log.push('two'));
    $('#c').click();
    krateOn($('#c'), 'onClick', null);
    $('#c').click();
    expect(log).toEqual(['two']);
  });

  it('runs every handler and rethrows the first error afterwards', () => {
    setup('<div id="a"><button id="c">x</button></div>');
    const log: string[] = [];
    krateOn($('#c'), 'onClick', () => {
      throw new Error('boom');
    });
    krateOn($('#a'), 'onClick', () => log.push('a'));
    const errors: unknown[] = [];
    const onErr = (e: ErrorEvent) => {
      errors.push(e.error ?? e.message);
      e.preventDefault();
    };
    window.addEventListener('error', onErr);
    try {
      $('#c').dispatchEvent(new Event('click', { bubbles: true }));
    } catch (e) {
      errors.push(e);
    }
    window.removeEventListener('error', onErr);
    expect(log).toEqual(['a']);
    expect(errors.length).toBe(1);
  });

  it('handles events in portals outside the root', () => {
    setup('');
    const portal = document.createElement('div');
    portal.innerHTML = '<button id="p">x</button>';
    document.body.appendChild(portal);
    const log: string[] = [];
    krateOn($('#p'), 'onClick', () => log.push('portal'));
    krateEventRoot(portal);
    $('#p').click();
    expect(log).toEqual(['portal']);
  });

  it('wires handlers set through h()', () => {
    const root = setup('');
    const log: string[] = [];
    const el = h('button', { onClick: (e: any) => log.push('h ' + e.type), onDoubleClick: () => log.push('dbl') });
    root.appendChild(el);
    (el as HTMLElement).click();
    fire(el, 'dblclick');
    expect(log).toEqual(['h click', 'dbl']);
  });
});

describe('onChange', () => {
  it('fires on every input event for text fields, after onInput', () => {
    setup('<div id="w"><input id="i"><textarea id="t"></textarea></div>');
    const log: string[] = [];
    krateOn($('#i'), 'onInput', () => log.push('input'));
    krateOn($('#i'), 'onChange', (e) => log.push('change ' + e.type + ' ' + (e.target as HTMLInputElement).value));
    krateOn($('#w'), 'onChange', () => log.push('wrapper change'));
    krateOn($('#t'), 'onChange', (e) => log.push('textarea ' + (e.target as HTMLTextAreaElement).value));
    const i = $('#i') as HTMLInputElement;
    i.value = 'a';
    fire(i, 'input');
    i.value = 'ab';
    fire(i, 'input');
    // The native change on blur carries no new value: no second onChange.
    fire(i, 'change');
    const t = $('#t') as HTMLTextAreaElement;
    t.value = 'x';
    fire(t, 'input');
    expect(log).toEqual([
      'input',
      'change change a',
      'wrapper change',
      'input',
      'change change ab',
      'wrapper change',
      'textarea x',
      'wrapper change',
    ]);
  });

  it('runs onChangeCapture before onChange', () => {
    setup('<div id="w"><input id="i"></div>');
    const log: string[] = [];
    krateOn($('#w'), 'onChangeCapture', () => log.push('w capture'));
    krateOn($('#i'), 'onChange', () => log.push('i'));
    krateOn($('#w'), 'onChange', () => log.push('w'));
    ($('#i') as HTMLInputElement).value = 'z';
    fire($('#i'), 'input');
    expect(log).toEqual(['w capture', 'i', 'w']);
  });

  it('fires on click for checkboxes and only for a newly checked radio', () => {
    setup('<input id="c" type="checkbox"><input id="r1" type="radio" name="g" checked><input id="r2" type="radio" name="g">');
    const log: string[] = [];
    krateOn($('#c'), 'onClick', () => log.push('click'));
    krateOn($('#c'), 'onChange', (e) => log.push('checkbox ' + (e.target as HTMLInputElement).checked));
    krateOn($('#r1'), 'onChange', () => log.push('r1'));
    krateOn($('#r2'), 'onChange', () => log.push('r2'));
    $('#c').click();
    $('#c').click();
    $('#r1').click();
    $('#r2').click();
    $('#r1').click();
    expect(log).toEqual(['click', 'checkbox true', 'click', 'checkbox false', 'r2', 'r1']);
  });

  it('fires on change for selects', () => {
    setup('<select id="s"><option value="a">A</option><option value="b">B</option></select>');
    const log: string[] = [];
    krateOn($('#s'), 'onChange', (e) => log.push((e.target as HTMLSelectElement).value));
    ($('#s') as HTMLSelectElement).value = 'b';
    fire($('#s'), 'input');
    fire($('#s'), 'change');
    expect(log).toEqual(['b']);
  });
});

describe('controlled form controls', () => {
  it('restores a controlled text input when the handler does not update state', async () => {
    setup('<input id="i" value="a">');
    const i = $('#i') as HTMLInputElement;
    const [v] = createSignal('a');
    krateControl(i, 'value', () => v());
    krateOn(i, 'onChange', () => {});
    i.value = 'ab';
    fire(i, 'input');
    expect(i.value).toBe('a');
  });

  it('restores even without an onChange handler (read-only controlled field)', () => {
    setup('<input id="i" value="a">');
    const i = $('#i') as HTMLInputElement;
    krateControl(i, 'value', () => 'a');
    i.value = 'typed';
    fire(i, 'input');
    expect(i.value).toBe('a');
  });

  it('keeps the typed value when state follows it, including transforms', async () => {
    setup('<input id="i" value="">');
    const i = $('#i') as HTMLInputElement;
    const [v, setV] = createSignal('');
    krateControl(i, 'value', () => v());
    krateOn(i, 'onChange', (e) => setV((e.target as HTMLInputElement).value.toUpperCase()));
    i.value = 'ab';
    fire(i, 'input');
    expect(i.value).toBe('AB');
    await tick();
    expect(i.value).toBe('AB');
    expect(i.defaultValue).toBe('AB');
  });

  it('restores a controlled checkbox and radio group', () => {
    setup('<input id="c" type="checkbox"><input id="r1" type="radio" name="g" checked><input id="r2" type="radio" name="g">');
    const c = $('#c') as HTMLInputElement;
    krateControl(c, 'checked', () => false);
    krateOn(c, 'onChange', () => {});
    c.click();
    expect(c.checked).toBe(false);

    const [sel, setSel] = createSignal('r1');
    const r1 = $('#r1') as HTMLInputElement;
    const r2 = $('#r2') as HTMLInputElement;
    krateControl(r1, 'checked', () => sel() === 'r1');
    krateControl(r2, 'checked', () => sel() === 'r2');
    krateOn(r2, 'onChange', () => {});
    r2.click();
    expect([r1.checked, r2.checked]).toEqual([true, false]);
    krateOn(r2, 'onChange', () => setSel('r2'));
    r2.click();
    expect([r1.checked, r2.checked]).toEqual([false, true]);
  });

  it('controls single and multiple selects', async () => {
    setup(
      '<select id="s"><option value="a">A</option><option value="b">B</option></select>' +
        '<select id="m" multiple><option value="a">A</option><option value="b">B</option><option value="c">C</option></select>'
    );
    const s = $('#s') as HTMLSelectElement;
    const m = $('#m') as HTMLSelectElement;
    const [v, setV] = createSignal('b');
    const [mv, setMv] = createSignal(['a', 'c']);
    krateControl(s, 'value', () => v());
    krateControl(m, 'value', () => mv());
    expect(s.value).toBe('b');
    expect(Array.from(m.options).filter((o) => o.selected).map((o) => o.value)).toEqual(['a', 'c']);
    setMv(['b']);
    setV('a');
    await tick();
    expect(s.value).toBe('a');
    expect(Array.from(m.options).filter((o) => o.selected).map((o) => o.value)).toEqual(['b']);
    // A handler that ignores the change: the select snaps back.
    krateOn(s, 'onChange', () => {});
    s.value = 'b';
    fire(s, 'change');
    expect(s.value).toBe('a');
  });

  it('treats value={undefined} as uncontrolled and warns on switching', async () => {
    setup('<input id="i">');
    const i = $('#i') as HTMLInputElement;
    const err = vi.spyOn(console, 'error').mockImplementation(() => {});
    const [v, setV] = createSignal<string | undefined>(undefined);
    krateControl(i, 'value', () => v());
    i.value = 'typed';
    fire(i, 'input');
    expect(i.value).toBe('typed');
    setV('now');
    await tick();
    expect(i.value).toBe('now');
    expect(err).toHaveBeenCalledTimes(1);
    expect(String(err.mock.calls[0][0])).toContain('changing an uncontrolled input to be controlled');
    err.mockRestore();
  });

  it('applies defaultValue/defaultChecked only as defaults; form reset restores them', async () => {
    setup('<form id="f"><input id="i" value="d"><input id="c" type="checkbox"></form>');
    const i = $('#i') as HTMLInputElement;
    const c = $('#c') as HTMLInputElement;
    const [d, setD] = createSignal('d');
    krateControl(i, 'defaultValue', () => d());
    krateControl(c, 'defaultChecked', () => true);
    i.value = 'user';
    setD('d2');
    await tick();
    expect(i.value).toBe('user');
    expect(i.defaultValue).toBe('d2');
    ($('#f') as HTMLFormElement).reset();
    expect(i.value).toBe('d2');
    expect(c.checked).toBe(true);
  });

  it('keeps number and range inputs controlled', () => {
    setup('<input id="n" type="number" value="1"><input id="r" type="range" min="0" max="10" value="5">');
    const n = $('#n') as HTMLInputElement;
    const r = $('#r') as HTMLInputElement;
    const [num, setNum] = createSignal(1);
    krateControl(n, 'value', () => num());
    krateControl(r, 'value', () => 5);
    const log: string[] = [];
    krateOn(n, 'onChange', (e) => {
      log.push((e.target as HTMLInputElement).value);
      setNum(Number((e.target as HTMLInputElement).value));
    });
    n.value = '7';
    fire(n, 'input');
    r.value = '8';
    fire(r, 'input');
    expect(log).toEqual(['7']);
    expect(n.value).toBe('7');
    expect(r.value).toBe('5');
  });
});
