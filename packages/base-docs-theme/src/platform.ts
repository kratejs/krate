/**
 * Keyboard shortcut labels that suit the platform. The page is built once, so
 * it carries both spellings and CSS shows one, keyed on the data-platform
 * attribute that PLATFORM_PREPAINT sets on <html> before first paint. Without
 * the script the non-Apple label shows, which is readable everywhere (the
 * command-key glyph is missing from most Linux and Windows fonts).
 */

/** Inline script body (no wrapper) that sets data-platform to "mac" or "other". */
export const PLATFORM_PREPAINT =
  "try{var q=navigator.userAgentData&&navigator.userAgentData.platform||navigator.platform||navigator.userAgent||'';" +
  "document.documentElement.setAttribute('data-platform',/Mac|iPhone|iPad|iPod/i.test(q)?'mac':'other');}catch(e){}";
