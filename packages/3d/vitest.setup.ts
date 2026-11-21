// The runtime code uses the page runtime from the global object; tests install
// it from the runtime sources, as @krate/three's tests do.
import * as signal from '../runtime/src/signal.ts';
import * as effects from '../runtime/src/effects.ts';
import * as dom from '../runtime/src/dom.ts';
import * as context from '../runtime/src/context.ts';

Object.assign(globalThis, signal, effects, dom, context);
