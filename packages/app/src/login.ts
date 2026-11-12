// Start the app when the user logs in. Needs the "login" permission.
import { call } from "./index.js";

export interface LoginItem {
  enabled: boolean;
  // How it is registered: "smappservice", "launchagent", "registry",
  // "startuptask" or "autostart"; "" when not enabled.
  method: string;
}

export interface LoginOptions {
  // Extra arguments passed at login, for example ["--background"].
  args?: string[];
}

export function getLoginItem(): Promise<LoginItem> {
  return call("krate/login.get", []);
}

export function setLoginItem(enabled: boolean, options: LoginOptions = {}): Promise<void> {
  return call("krate/login.set", [enabled, { args: options.args ?? [] }]);
}
