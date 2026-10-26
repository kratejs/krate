import { jsx } from 'react/jsx-runtime';

export function IslandLocalBadge({ label }: { label: string }) {
  return jsx('span', { className: 'island-local-badge', children: label });
}
