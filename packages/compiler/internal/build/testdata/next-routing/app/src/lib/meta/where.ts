import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// A module in another directory than its route asks where it lives and reads a file next to it.
export function where() {
  return {
    endsWith: fileURLToPath(import.meta.url).split(path.sep).join('/').endsWith('/lib/meta/where.ts'),
    read: fs.readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'data', 'x.txt'), 'utf8').trim(),
  };
}
