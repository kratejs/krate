---
title: Analytics
description: Add analytics snippets and hook into SPA route changes.
order: 11
---

# Analytics

Add analytics to a Krate site with a snippet in a layout and the
`__krate_onNavigation` hook for SPA route changes.

## Injecting the snippet

Put the embed script in a layout (or any component) so it is present on every
page:

```tsx
// src/layouts/RootLayout.tsx
export default function RootLayout({ children }) {
  return (
    <>
      <Script src="https://plausible.io/js/script.js" data-domain="mysite.com" />
      <div>{children}</div>
    </>
  );
}
```

`<Script>` forwards all attributes, so flags like `defer` and `data-domain`
work as expected. If your provider needs an inline init snippet, use the
inline form — children are written raw and never escaped:

```tsx
<Script>{`gtag('config', 'G-XXXXXXXXXX');`}</Script>
```

## Tracking SPA navigations

Krate's client router swaps content in place, so an ordinary tag-based
snippet only fires on the initial full page load. Register a navigation hook
once — for example in a layout script or the root page — and it runs on
**every successful SPA navigation**:

```tsx
<Script>{`
  window.__krate_onNavigation = (url) => {
    if (window.plausible) window.plausible('pageview', { u: url });
  };
`}</Script>
```

For Google Analytics:

```tsx
<Script>{`
  window.__krate_onNavigation = (url) => {
    if (window.gtag) window.gtag('event', 'page_view', {
      page_location: url,
      page_title: document.title,
    });
  };
`}</Script>
```

Hook contract:

- **Signature** — `(url: string) => void`; `url` is the resolved navigation
  target (absolute), the same URL passed to the `krate:navigate` CustomEvent.
- **Fires once per navigation** — click, prefetch-cache hit, back/forward,
  and error-page replacements. The initial page load is a full page load and
  is tracked by the snippet's usual beacon — do not double-count it.
- **Fail-safe** — a throw inside the hook is caught and ignored so analytics
  can never break routing.
- **Set before navigating** — assign it once at app bootstrap (e.g. in a
  layout `<Script>`); it persists across navigations.

## Alternatives

The same event is also available as a DOM event, which some analytics wrappers
consume directly:

```js
window.addEventListener('krate:navigate', (e) => {
  // e.detail.url — the navigated-to URL
});
```