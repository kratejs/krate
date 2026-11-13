import { describe, it, expect, beforeEach } from 'vitest';
import { krateOn, krateControl } from './events';
import { createSignal } from './signal';


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
});
