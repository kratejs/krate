import { describe, it, expect, beforeEach } from 'vitest';
import { krateMountPortals, kratePortalFind, kratePortalReset, kratePortalScan, kratePortalTarget } from './portal';
import { krateCondSwap } from './slots';
import { krateOn } from './events';
import { disposeNode } from './dom';
import { createPortal } from './react';

function startMarker(root: ParentNode, id: string): Comment {
  const w = document.createTreeWalker(root as Node, NodeFilter.SHOW_COMMENT);
  while (w.nextNode()) {
    if (w.currentNode.nodeValue === 'k:' + id) return w.currentNode as Comment;
  }
  throw new Error('marker not found: ' + id);
}

const bodyChildren = () => Array.from(document.body.children).map((el) => el.id || el.tagName.toLowerCase());

describe('portals', () => {
  beforeEach(() => {
    kratePortalReset();
    document.body.innerHTML = '';
  });

  it('mounts portal content into body and bubbles events through the React tree', () => {
    document.body.innerHTML =
      '<div id="root"><div id="host"><template data-krate-portal="p1"><div id="toast"><button id="btn">ok</button></div></template></div></div>';
    const host = document.getElementById('host')!;
    const btn = (document.querySelector('template') as HTMLTemplateElement).content.querySelector('#btn')!;
    const seen: string[] = [];
    krateOn(btn, 'onClick', () => seen.push('button'));
    krateOn(host, 'onClick', () => seen.push('host'));

    krateMountPortals();
    expect(bodyChildren()).toEqual(['root', 'toast']);
    expect(document.getElementById('btn')).toBe(btn);

    (btn as HTMLElement).click();
    expect(seen).toEqual(['button', 'host']);
  });

  it('mounts into a selector or registered container', () => {
    document.body.innerHTML =
      '<div id="root"><template data-krate-portal="a" data-krate-portal-target="#modal-root"><p id="a">a</p></template>' +
      '<template data-krate-portal="b"><p id="b">b</p></template></div><div id="modal-root"></div><div id="side"></div>';
    kratePortalTarget('b', () => document.getElementById('side'));
    krateMountPortals();
    expect(document.getElementById('a')!.parentElement!.id).toBe('modal-root');
    expect(document.getElementById('b')!.parentElement!.id).toBe('side');
  });

  it('follows its conditional branch: unmounts when hidden, remounts when shown', () => {
    document.body.innerHTML =
      '<div id="root"><!--k:c--><!--/k:c--></div>' +
      '<template data-kc="c" data-kb="c"><template data-krate-portal="m"><div id="modal">hi</div></template></template>';
    const start = startMarker(document.body, 'c');
    const tpl = document.querySelector('template[data-kc]') as HTMLTemplateElement;
    krateCondSwap(start, false, tpl);
    krateMountPortals();
    expect(document.getElementById('modal')).toBeNull();

    krateCondSwap(start, true, tpl);
    expect(bodyChildren()).toContain('modal');
    const modal = document.getElementById('modal');

    krateCondSwap(start, false, tpl);
    expect(document.getElementById('modal')).toBeNull();

    krateCondSwap(start, true, tpl);
    expect(document.getElementById('modal')).toBe(modal);
  });

  it('drops the content when the anchor is disposed', () => {
    document.body.innerHTML = '<div id="root"><span id="wrap"><template data-krate-portal="d"><i id="x">x</i></template></span></div>';
    krateMountPortals();
    expect(document.getElementById('x')).not.toBeNull();
    disposeNode(document.getElementById('wrap')!);
    expect(document.getElementById('x')).toBeNull();
  });

  it('finds bound nodes in portal templates before and after mounting', () => {
    document.body.innerHTML = '<div id="root"><template data-krate-portal="f"><b data-k="k:9">b</b><!--k:8--><!--/k:8--></template></div>';
    const root = document.getElementById('root')!;
    const before = kratePortalFind('[data-k="k:9"]', [root]);
    expect(before).not.toBeNull();
    const comments: string[] = [];
    kratePortalScan([root], (content) => {
      const w = document.createTreeWalker(content, NodeFilter.SHOW_COMMENT);
      while (w.nextNode()) comments.push(String(w.currentNode.nodeValue));
    });
    expect(comments).toContain('k:8');
    krateMountPortals();
    expect(kratePortalFind('[data-k="k:9"]', [root])).toBe(before);
  });

  it('runtime createPortal returns an anchor and removes content on dispose', () => {
    document.body.innerHTML = '<div id="root"><div id="host"></div></div>';
    const host = document.getElementById('host')!;
    const content = document.createElement('p');
    content.id = 'rt';
    const seen: string[] = [];
    krateOn(host, 'onClick', () => seen.push('host'));
    const anchor = createPortal(content, document.body);
    host.appendChild(anchor);
    expect(anchor.nodeType).toBe(8);
    expect(content.parentNode).toBe(document.body);
    content.click();
    expect(seen).toEqual(['host']);
    disposeNode(anchor);
    expect(document.getElementById('rt')).toBeNull();
  });
});
