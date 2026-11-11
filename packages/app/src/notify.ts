// Desktop notifications. Needs the "notify" permission.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export interface NotificationAction {
  id: string;
  label: string;
}

export interface Notification {
  // Sending the same id again replaces the notification; events carry it.
  id?: string;
  title: string;
  body?: string;
  // Icon name from the theme, or an absolute file path.
  icon?: string;
  actions?: NotificationAction[];
  urgency?: "low" | "normal" | "critical";
  // Milliseconds to show it; 0 is the server default, negative means until dismissed.
  timeoutMs?: number;
  silent?: boolean;
}

export interface NotificationEvent {
  id: string;
  // Action id, "" for a click on the notification itself.
  action: string;
  // True when it went away without an action.
  closed: boolean;
}

const URGENCY = { low: 0, normal: 1, critical: 2 } as const;

export function send(n: Notification): Promise<void> {
  return call("krate/notify.Send", [
    {
      id: n.id ?? "",
      title: n.title,
      body: n.body ?? "",
      icon: n.icon ?? "",
      actions: n.actions ?? [],
      urgency: URGENCY[n.urgency ?? "normal"],
      timeoutMs: n.timeoutMs ?? 0,
      silent: n.silent ?? false,
    },
  ]);
}

export function close(id: string): Promise<void> {
  return call("krate/notify.Close", [id]);
}

export function onEvent(fn: (e: NotificationEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<NotificationEvent>("krate/notify.events", fn, opts);
}

// onAction calls fn when the action (or, with "", the notification body) of
// the notification with this id is chosen.
export function onAction(id: string, action: string, fn: () => void, opts?: SubscribeOptions): () => void {
  return onEvent((e) => {
    if (e.id === id && e.action === action && !e.closed) fn();
  }, opts);
}
