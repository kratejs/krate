// Offscreen rendering: windows that render out of sight, and frames of any
// window. Needs the "offscreen" permission.
//
// The system webviews cannot push frames the way Chromium's offscreen mode
// does. A window opened here is never shown (a GtkOffscreenWindow on Linux, a
// window parked off every display on Windows and macOS) and its page renders
// normally; frames are snapshots (WebKitGTK get_snapshot, WKWebView
// takeSnapshot, WebView2 CapturePreview) taken one at a time at the requested
// rate. A slow consumer makes the stream skip frames, counted in
// Frame.dropped, instead of queueing them.
import { call, stream } from "./index.js";

export interface OffscreenOptions {
  // The route to render, "/" by default.
  path?: string;
  width?: number;
  height?: number;
}

export interface CaptureOptions {
  // "png" (default) or "bgra": premultiplied pixels, width * 4 bytes per row.
  format?: "png" | "bgra";
  // Capture the whole page instead of the visible area (WebKitGTK only).
  fullDocument?: boolean;
  // Frames per second of a stream: default 10, at most 60.
  fps?: number;
  // End a stream after this many frames.
  maxFrames?: number;
}

export interface Frame {
  seq: number;
  // Unix time in milliseconds when the frame was taken.
  timeMs: number;
  width: number;
  height: number;
  format: "png" | "bgra";
  // Frames skipped since the previous one because the capture or the consumer
  // was too slow.
  dropped: number;
  // The PNG file, or the raw pixels.
  data: Uint8Array;
}

// openOffscreen creates a window that is never shown and returns its id. Use
// the window module's navigate and close with it, and window.thisView in its
// page if it needs its own id.
export function openOffscreen(options: OffscreenOptions = {}): Promise<string> {
  return call("krate/offscreen.open", [options]);
}

// captureFrame takes one frame of a window or view ("" is the main window).
export function captureFrame(window = "", options: CaptureOptions = {}): Promise<Frame> {
  return call("krate/offscreen.capture", [window, options]);
}

// frames yields frames of a window or view at options.fps until the loop is
// left, the signal aborts, or maxFrames is reached. The next frame is taken
// only after the consumer returned for the last one (backpressure).
export function frames(window = "", options: CaptureOptions = {}, signal?: AbortSignal): AsyncIterable<Frame> {
  return stream<Frame>("krate/offscreen.frames", [window, options], { signal });
}
