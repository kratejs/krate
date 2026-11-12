// Cookies and site data. Needs the "cookies" permission.
//
// Cookies are addressed by URL (default: the app origin). WebKit (Linux and
// macOS) keeps no cookies for the app's own custom scheme, so there these
// calls reject with code "unsupported" unless you pass the URL of an http(s)
// site, for example the API your pages fetch; on Windows the app origin is an
// https origin and has cookies. clearSiteData works everywhere.
import { call } from "./index.js";

// The functions are declared under prefixed names and exported under their short ones:
// the bundler hoists top-level declarations of every module into one scope, so two modules
// that both declare a `get` or `list` would call each other when imported as namespaces.

export type SameSite = "none" | "lax" | "strict" | "";

export interface Cookie {
  name: string;
  value: string;
  domain: string;
  path: string;
  /** Expiry in milliseconds since the epoch, null for a session cookie. */
  expires: number | null;
  secure: boolean;
  httpOnly: boolean;
  sameSite: SameSite;
}

export interface CookieInput {
  name: string;
  value: string;
  /** Default: the host of the URL. */
  domain?: string;
  /** Default "/". */
  path?: string;
  expires?: number | Date | null;
  secure?: boolean;
  httpOnly?: boolean;
  sameSite?: SameSite;
}

export interface ClearOptions {
  cookies?: boolean;
  /** The HTTP and memory cache. */
  cache?: boolean;
  /** localStorage, sessionStorage, IndexedDB and service workers. */
  storage?: boolean;
}

/** list resolves to the cookies that would be sent to the URL. */
function cookiesList(url = ""): Promise<Cookie[]> {
  return call("krate/cookies.list", [url]);
}

/** get resolves to the cookie with this name, or null. */
function cookiesGet(name: string, url = ""): Promise<Cookie | null> {
  return call("krate/cookies.get", [name, url]);
}

/** set stores a cookie. */
function cookiesSet(cookie: CookieInput, url = ""): Promise<void> {
  const expires = cookie.expires instanceof Date ? cookie.expires.getTime() : (cookie.expires ?? null);
  return call("krate/cookies.set", [
    {
      name: cookie.name,
      value: cookie.value,
      domain: cookie.domain ?? "",
      path: cookie.path ?? "",
      expires,
      secure: cookie.secure ?? false,
      httpOnly: cookie.httpOnly ?? false,
      sameSite: cookie.sameSite ?? "",
    },
    url,
  ]);
}

/** delete removes the cookies with this name. */
function cookiesDelete(name: string, url = ""): Promise<void> {
  return call("krate/cookies.delete", [name, url]);
}
export { cookiesDelete as delete };

/** clearSiteData removes cookies, cache and storage; with no options, all of it. */
function cookiesClearSiteData(options: ClearOptions = {}): Promise<void> {
  return call("krate/cookies.clearSiteData", [
    { cookies: options.cookies ?? false, cache: options.cache ?? false, storage: options.storage ?? false },
  ]);
}

export { cookiesList as list, cookiesGet as get, cookiesSet as set, cookiesClearSiteData as clearSiteData };
