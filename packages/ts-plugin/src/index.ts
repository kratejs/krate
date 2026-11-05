import { init, isTierDiagnostic } from './plugin';

// tsserver calls whatever `require` returns, so the module itself is the factory.
export = Object.assign(init, { default: init, isTierDiagnostic });
