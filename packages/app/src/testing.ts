import { clearMocks, setMock } from "./mock.js";
import type { Impl } from "./types.js";

export { createFakeHost } from "./fakehost.js";
export type { FakeHost, FakeHandler } from "./fakehost.js";

// mockHost routes call/stream/signal for one module to impl, with no network.
export function mockHost(moduleId: string, impl: Impl): void {
  setMock(moduleId, impl);
}

export function resetHostMocks(): void {
  clearMocks();
}
