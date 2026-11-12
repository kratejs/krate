// Camera, microphone, screen capture and location for the page, with
// app-controlled permissions. Reading the state needs the "media" permission;
// changing it, answering requests and listing screens needs "media:manage".
// The page's own getUserMedia / getDisplayMedia / geolocation calls need
// "media" (camera, microphone, screen) or "location" in the window's scope.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export type PermissionKind = "camera" | "microphone" | "screen" | "location" | "notifications" | "clipboardRead";
export type PermissionPolicy = "prompt" | "allow" | "deny";
export type Remember = "app" | "view" | "none";
// The operating system kinds that have a consent switch.
export type OSConsentKind = "camera" | "microphone" | "screen" | "location";
export type OSConsent = "granted" | "denied" | "notDetermined" | "restricted" | "unknown";

export type MediaPolicy = Record<PermissionKind, PermissionPolicy>;

export interface PermissionDecision {
  kind: PermissionKind;
  // "app", or the id of the window or view.
  scope: string;
  allow: boolean;
  at: Date | string;
}

export interface PermissionRequest {
  id: number;
  // Id of the window or view that asked.
  view: string;
  origin: string;
  kinds: PermissionKind[];
}

export interface PermissionOutcome extends PermissionRequest {
  allowed: boolean;
  // remembered, policy, prompt, handler, scope, timeout, unknown view,
  // unsupported, os or no handler.
  reason: string;
}

export interface Verdict {
  allow: boolean;
  // "app" (default), "view" or "none".
  remember?: Remember;
}

export interface CaptureSource {
  id: string;
  kind: "screen" | "window";
  name: string;
  app: string;
  width: number;
  height: number;
  // PNG bytes, null without a thumbnail.
  thumbnail: Uint8Array | null;
}

// The policy of each kind from native.media.
export function getPolicy(): Promise<MediaPolicy> {
  return call("krate/media.policy", []);
}

// Set what happens to undecided requests of a kind.
export function setPolicy(kind: PermissionKind, policy: PermissionPolicy): Promise<void> {
  return call("krate/media.setPolicy", [kind, policy]);
}

// The remembered decisions.
export function decisions(): Promise<PermissionDecision[]> {
  return call("krate/media.decisions", []);
}

// Store a decision without waiting for a request. scope is "app" or a window
// or view id.
export function decide(kind: PermissionKind, allow: boolean, scope = "app"): Promise<void> {
  return call("krate/media.decide", [kind, scope, allow]);
}

// Forget the decisions of a kind, or of all kinds.
export function reset(kind?: PermissionKind): Promise<void> {
  return call("krate/media.reset", kind === undefined ? [] : [kind]);
}

// Decide permission requests. While a handler is subscribed, a request that no
// remembered decision covers is passed to it. The handler has a minute to
// answer; a throw or a timeout denies.
export function onRequest(
  handler: (r: PermissionRequest) => Verdict | boolean | Promise<Verdict | boolean>,
  opts?: SubscribeOptions,
): () => void {
  return subscribe<PermissionRequest & { pending?: boolean }>(
    "krate/media.requests",
    (r) => {
      void (async () => {
        let v: Verdict | boolean = false;
        try {
          v = await handler(r);
        } catch (e) {
          console.error(e);
        }
        const verdict: Verdict = typeof v === "boolean" ? { allow: v } : v;
        await call("krate/media.respond", [r.id, verdict.allow, verdict.remember ?? ""]).catch(() => {});
      })();
    },
    opts,
  );
}

// Called when a request ended, whoever decided it.
export function onOutcome(fn: (o: PermissionOutcome) => void, opts?: SubscribeOptions): () => void {
  return subscribe<PermissionOutcome>("krate/media.events", fn, opts);
}

// The operating system's consent state, without prompting.
export function osStatus(kind: OSConsentKind): Promise<OSConsent> {
  return call("krate/media.osStatus", [kind]);
}

// Show the operating system's consent prompt when it was not answered yet.
export function osRequest(kind: OSConsentKind): Promise<OSConsent> {
  return call("krate/media.osRequest", [kind]);
}

// Screens and windows that can be captured, with thumbnails of the given
// width. Fails with an "unsupported" error on a platform that cannot list them
// (Linux: the portal picker is the only way).
export function sources(opts: { thumbnailWidth?: number } = {}): Promise<CaptureSource[]> {
  return call("krate/media.sources", [{ thumbnailWidth: opts.thumbnailWidth ?? 0 }]);
}

// Would choose the source of the next getDisplayMedia call. No engine accepts
// that, so it always fails with an "unsupported" error and the system picker
// decides.
export function chooseSource(id: string): Promise<void> {
  return call("krate/media.chooseSource", [id]);
}
