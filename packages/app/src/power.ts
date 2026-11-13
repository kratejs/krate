// Power state, idle time and sleep blockers.
// Reading needs "power"; preventSleep needs "power:inhibit".
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export interface PowerState {
  onBattery: boolean;
  // Seconds since the last keyboard or pointer input; -1 when unknown.
  idleSeconds: number;
  locked: boolean;
  // "active" or "idle" (relative to setIdleThreshold), "locked" or "unknown".
  idleState: "active" | "idle" | "locked" | "unknown";
}

export type PowerEventKind = "suspend" | "resume" | "lock" | "unlock" | "ac" | "battery" | "idle" | "active";

export interface PowerEvent {
  kind: PowerEventKind;
}

export function powerState(): Promise<PowerState> {
  return call("krate/power.state", []);
}

// idleTime resolves the seconds since the last input, or -1 when the platform
// cannot tell.
export function idleTime(): Promise<number> {
  return call("krate/power.idleTime", []);
}

// setIdleThreshold makes "idle" and "active" events fire when the idle time
// crosses it. 0 turns them off (the default).
export function setIdleThreshold(seconds: number): Promise<void> {
  return call("krate/power.setIdleThreshold", [seconds]);
}

// preventSleep keeps the system awake (and the display on, with display)
// until allowSleep(id) or until the app exits. Resolves the blocker id.
export function preventSleep(reason: string, display = false): Promise<string> {
  return call("krate/power.preventSleep", [reason, display]);
}

export function allowSleep(id: string): Promise<void> {
  return call("krate/power.allowSleep", [id]);
}

export function onPowerEvent(fn: (e: PowerEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<PowerEvent>("krate/power.events", fn, opts);
}

// on calls fn for one kind of event.
export function onPower(kind: PowerEventKind, fn: () => void, opts?: SubscribeOptions): () => void {
  return onPowerEvent((e) => {
    if (e.kind === kind) fn();
  }, opts);
}
