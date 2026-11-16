// The engine uses the page runtime from the global object. In a page the
// runtime bundle attaches its whole API to window; the tests do the same with
// the runtime sources (the npm entry exports only the public subset).
import * as signal from '../runtime/src/signal.ts';
import * as effects from '../runtime/src/effects.ts';
import * as dom from '../runtime/src/dom.ts';
import * as list from '../runtime/src/list.ts';
import * as context from '../runtime/src/context.ts';

Object.assign(globalThis, signal, effects, dom, list, context, { IS_KRATE_THREE_TEST: true });
