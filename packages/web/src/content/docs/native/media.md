---
title: Camera, Microphone and Location
order: 16
description: Camera, microphone, screen capture and location in Krate Native apps with app-controlled permissions - the policy, remembered decisions, the request handler, operating system consent, usage descriptions and screen sources.
---

# Camera, microphone and location

> **Preview.** Part of [Krate Native](/docs/native/). The Linux (WebKitGTK)
> request path is tested end to end with mock capture devices. The macOS
> (WKWebView) and Windows (WebView2) paths compile and their decision tables are
> unit-tested, and the CI native job drives their request path, but nothing here
> has been confirmed on a real Mac or Windows machine. No CI runner has a camera
> or microphone, so no test records real hardware.

A page asks the webview for the camera, the microphone, screen capture and the
location with `getUserMedia`, `getDisplayMedia` and `navigator.geolocation`.
A browser decides with its own prompt. In a Krate Native app **the app
decides**: the platform layer hands every such request to the SDK, and nothing
is granted that the app did not allow.

```ts
// krate.config.ts
export default {
  native: {
    media: {
      camera: "prompt",      // "prompt" (default) | "allow" | "deny"
      microphone: "prompt",
      screen: "prompt",
      location: "deny",
      notifications: "prompt",
      clipboardRead: "prompt",
      remember: true,        // keep the answers of the native prompt (default)
      usage: {               // macOS Info.plist usage descriptions
        camera: "Acme shows your camera in video calls.",
        microphone: "Acme records your voice in video calls.",
        location: "Acme tags notes with where you wrote them.",
        screenCapture: "Acme shares your screen in calls.",
      },
    },
  },
};
```

`krate native build` writes the policy to `app.json` as `media`; the shell reads
it once at load.

## How a request is decided

For each request (one `getUserMedia({ audio: true, video: true })` is one
request with two kinds on Linux and macOS, two requests on Windows):

1. **Scope.** The window or view that asked must have the matching Krate
   permission in its [scope](/docs/native/release/#per-window-scopes): `media`
   for camera, microphone and screen capture, `location` for the location,
   `notify` for notifications and `clipboard:read` for reading the clipboard. A
   request from a view without it is denied, whatever the policy says. The view
   is the native webview that raised the request, not anything the page sends.
2. **Remembered decisions.** A decision stored for the view, then one for the
   app, answers without asking. A remembered denial denies.
3. **The request stream.** While the app has a handler subscribed
   (`onRequest` in TypeScript, `media.OnRequest` in Go), a request nothing
   remembered goes to it and only to it. It has a minute to answer; silence
   denies. The answer can be remembered for the app, for the view or not at all.
4. **The policy.** `allow` and `deny` decide. `prompt` shows a native message
   dialog ("Acme wants to use your camera.", Allow or Deny). With no dialog
   available (no `dialog` capability on the platform) a prompt denies. Go host
   code can replace the dialog with its own UI through `media.SetPrompt`.
5. **Operating system consent.** An allowed camera, microphone, screen or
   location request then asks the OS (next section). A refusal there denies the
   request.

A request type the SDK does not manage is left to the engine (pointer lock, the
sensor and download permissions of WebView2). Request types with no kind (the
WebKitGTK device-info request that `enumerateDevices` raises before capture is
allowed) are denied, so the page sees device labels only after it holds a
capture permission, as in other browsers.

### Permissions

| Permission | Grants |
|------------|--------|
| `media` | the page may use the camera, microphone and screen capture (subject to the steps above); reading the policy, the remembered decisions and the OS consent state |
| `media:manage` | changing the policy, remembering and forgetting decisions, answering requests, asking the OS, listing screens and windows; implies `media` |
| `location` | the page may read the device location |

None is a default. **A derived build adds `media` and `location` for you**: the
build scans the built UI for `getUserMedia` / `getDisplayMedia` and for
`navigator.geolocation`, like it derives permissions from the SDK ids the code
calls, and lists them in `app.json` (and reports them against
`native.permissions` as KR7210 when that allowlist lacks them). `krate native
dev` does the same for an app without a `native.permissions` list; with a list
that lacks the name, the build prints a note, because the shell will deny the
request and the page only sees `NotAllowedError`. `media:manage` is never
derived by the scan; the SDK ids the code calls decide it.

## Seeing and changing it from the app

```ts
import * as media from "@krate/app/media";

// Decide requests yourself. While this is subscribed, the policy and the
// dialog are not used for kinds nothing remembered.
const stop = media.onRequest(async (req) => {
  // req = { id, view, origin, kinds: ["camera", "microphone"] }
  const ok = await askMyOwnWay(req.kinds);
  return { allow: ok, remember: "app" }; // or a plain true / false
});

await media.getPolicy();                 // { camera: "prompt", ... }
await media.setPolicy("location", "deny");
await media.decisions();                 // [{ kind, scope: "app" | viewId, allow, at }]
await media.decide("camera", true);      // remember without a request
await media.reset("camera");             // forget one kind; reset() forgets all
media.onOutcome((o) => console.log(o.kinds, o.allowed, o.reason));
```

`reason` says who decided: `remembered`, `policy`, `prompt`, `handler`, `scope`,
`timeout`, `os`, `unknown view`, `unsupported` or `no handler`.

App-wide decisions are stored in `permissions.json` in the app data directory
(mode 0600); decisions for a view live as long as the view. A corrupt file is
ignored.

In Go host code (linked into the shell, or in the Go host child, which reaches
the shell over its socket):

```go
import "github.com/kratejs/krate/packages/native/sdk/media"

cancel, _ := media.OnRequest(func(r media.Request) media.Verdict {
    return media.Verdict{Allow: r.View == "main", Remember: "none"}
})
defer cancel()

media.SetPolicy("microphone", "deny")
ds, _ := media.Decisions()
media.Reset("") // forget everything
```

## Operating system consent

An allowed request still needs the operating system's consent.

| | Camera, microphone | Screen | Location |
|--|--|--|--|
| **macOS** | `AVCaptureDevice` authorization status and `requestAccess` | `CGPreflightScreenCaptureAccess` / `CGRequestScreenCaptureAccess` | `CLLocationManager` |
| **Windows** | capability consent store (HKLM and HKCU `CapabilityAccessManager\ConsentStore\webcam`, `microphone`, and `NonPackaged` for desktop apps), read only | no switch | `...\ConsentStore\location`, read only |
| **Linux** | the Camera portal (`org.freedesktop.portal.Camera.AccessCamera`) when the app runs under Flatpak or Snap; otherwise no switch | the ScreenCast portal, driven by WebKitGTK | GeoClue, driven by WebKitGTK |

`media.osStatus(kind)` reads the state without prompting (`granted`, `denied`,
`notDetermined`, `restricted` or `unknown`) and `media.osRequest(kind)` shows
the system prompt when there is one. `unknown` means the system keeps no switch
the app can read, and the request is allowed on from there.

Limits:

- macOS reports screen recording as granted or not yet granted. The first
  `CGRequestScreenCaptureAccess` shows the system prompt; the grant takes
  effect after the app restarts, so the first screen capture of a fresh install
  is refused.
- Windows has no API that makes an unpackaged desktop app show a consent
  prompt; `osRequest` returns the current state. A denied state needs the user
  in Settings, Privacy and security.
- A Linux desktop outside a sandbox has no consent switch for the camera or
  microphone at all.

### Usage descriptions and capabilities

`krate native build` finds what the UI uses and `krate native package` writes
it into the package:

- **macOS** `Info.plist`: `NSCameraUsageDescription`,
  `NSMicrophoneUsageDescription`, `NSLocationUsageDescription` and
  `NSLocationWhenInUseUsageDescription` (and `NSScreenCaptureUsageDescription`)
  from `native.media.usage`. A feature the UI uses without a sentence gets a
  generic one, because macOS ends the process otherwise, and the build reports
  **KR7290** (a warning on builds that target macOS). The generated hardened
  runtime entitlements get `com.apple.security.device.camera`,
  `com.apple.security.device.audio-input` and
  `com.apple.security.personal-information.location` for the devices the UI
  uses; without them macOS delivers no data under the hardened runtime and
  shows no prompt.
- **Windows MSIX**: `<DeviceCapability Name="webcam">`, `microphone` and
  `location` for the devices the UI uses, so a packaged app appears in the
  Windows privacy settings.

## Screen and window capture

`media.sources({ thumbnailWidth })` lists the screens and windows that can be
captured, with PNG thumbnails (`media:manage`):

| Platform | Listing |
|----------|---------|
| macOS | CoreGraphics (`CGGetActiveDisplayList`, `CGWindowListCopyWindowInfo`, thumbnails from `CGDisplayCreateImage` and `CGWindowListCreateImage`). Window titles and images need the Screen Recording permission. ScreenCaptureKit is not used: it is asynchronous and the picker below cannot take its result. |
| Windows | `EnumDisplayMonitors` and `EnumWindows` (visible, uncloaked, titled, non-tool windows); thumbnails with GDI (`BitBlt`, `PrintWindow`). |
| Linux | not available: the call returns `unsupported`. Under Wayland and X11 alike WebKitGTK reaches the screen through the ScreenCast portal and PipeWire, whose picker is the only list. |

**No engine accepts a source chosen by the app.** `getDisplayMedia` always shows
the system picker (the WebView2 and WKWebView pickers, the ScreenCast portal),
so `media.chooseSource(id)` returns `unsupported` everywhere and the list is for
the app's own UI. What the app can do is gate it: a screen capture request is a
permission request of kind `screen`, decided as above. WebView2 raises it as
`ScreenCaptureStarting` (ICoreWebView2_27); a runtime without that interface
cannot be gated and its own picker is the only control. WKWebView raises it
through a private WebKit delegate method
(`_webView:requestDisplayCapturePermissionForOrigin:initiatedByFrame:withSystemAudio:decisionHandler:`).

## Engine details

| | Request hook | Notes |
|--|--|--|
| Linux | `permission-request` signal of each `WebKitWebView`: user media (audio, video, display), geolocation, notification and clipboard requests | The request is kept and answered later, so a dialog can be shown. With no handler the request is denied. The `webrtc` feature (see [Memory](/docs/native/memory/#features)) is on when the code mentions `getUserMedia`, which turns on `enable-media-stream`. WebKitGTK gets positions from GeoClue; without it an allowed `getCurrentPosition` fails with `POSITION_UNAVAILABLE`, which is not a permission error. |
| macOS | `KrateUIDelegate`: `webView:requestMediaCapturePermissionForOrigin:initiatedByFrame:type:decisionHandler:` (macOS 12+), the display capture method above, and `requestGeolocationPermission...` where WebKit calls it | The decision handler block is copied and called once on the main thread. |
| Windows | `PermissionRequested` per view, with a deferral; `SavesInProfile` is turned off (`ICoreWebView2PermissionRequestedEventArgs3`) so WebView2 never answers a request itself from its profile | The app owns the remembered decisions. `getUserMedia({ audio, video })` raises two events, one per device. |

## Testing

Real devices are not available in CI, so:

- **Linux**: set `KRATE_NATIVE_MOCK_CAPTURE=1` and WebKitGTK uses its mock
  capture devices (`enable-mock-capture-devices`); they need the GStreamer base
  plugins (`libgstapp`, `libgstvideotestsrc`) that `libwebkit2gtk` depends on.
  `TestGUIPermission*` in `platform/linux` and `TestNativeE2EMediaGUI` use it.
- **Windows**: `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--use-fake-device-for-media-stream`
  gives Chromium fake devices. `TestGUIPermission*` in `platform/windows` use it.
- **macOS**: a runner has no camera, so WebKit rejects `getUserMedia` before it
  asks the app. `TestGUIPermissionDelegate` calls the delegate methods of the
  real web view with blocks of its own, as WebKit does.
- In UI tests `@krate/app/testing` and `createFakeHost` serve the
  `krate/media.*` ids.

## Known limits

- The macOS and Windows implementations are untested on those systems. The
  Objective-C selectors, the WebView2 interfaces (their vtables checked against
  `WebView2.h`) and the registry layout were written from
  the headers and documentation.
- Notifications and clipboard-read requests are routed on Linux and Windows;
  macOS WKWebView has no public delegate for them.
- A policy of `allow` for the microphone on a page the user does not trust is
  the app's decision: the SDK does not tell origins apart because the app
  origin is the only one the navigation lock lets in.
