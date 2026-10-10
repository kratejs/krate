import { describe, it, expect } from 'vitest';
import { Card } from '../src/card/card';
import { Input } from '../src/input/input';
import { Textarea } from '../src/textarea/textarea';
import { Label } from '../src/label/label';
import { Separator } from '../src/separator/separator';
import { AspectRatio } from '../src/aspect-ratio/aspect-ratio';
import { Avatar } from '../src/avatar/avatar';
import { Progress } from '../src/progress/progress';
import { VisuallyHidden } from '../src/visually-hidden/visually-hidden';
import { LinkCard } from '../src/link-card/link-card';

// Smoke tests: each component renders to a real DOM node with its base class.
describe('component smoke', () => {
  const cases: Array<[string, () => Element, string, string?]> = [
    ['Card', () => Card({ children: 'body' }), 'krate-card', 'body'],
    ['Input', () => Input({}), 'krate-input'],
    ['Textarea', () => Textarea({}), 'krate-textarea'],
    ['Label', () => Label({ children: 'Name' }), 'krate-label', 'Name'],
    ['Separator', () => Separator({}), 'krate-separator'],
    ['AspectRatio', () => AspectRatio({ ratio: 2, children: 'x' }), 'krate-aspect-ratio'],
    ['Avatar', () => Avatar({ fallback: 'AB' }), 'krate-avatar'],
    ['Progress', () => Progress({ value: 50, max: 100 }), 'krate-progress'],
    ['VisuallyHidden', () => VisuallyHidden({ children: 'a11y' }), 'krate-visually-hidden'],
    ['LinkCard', () => LinkCard({ href: '/x', title: 'T' }), 'krate-link-card'],
  ];

  for (const [name, render, cls, text] of cases) {
    it(`${name} renders`, () => {
      const el = render();
      expect(el).toBeInstanceOf(HTMLElement);
      expect(el.className).toContain(cls);
      if (text !== undefined) {
        expect(el.textContent).toContain(text);
      }
    });
  }
});
