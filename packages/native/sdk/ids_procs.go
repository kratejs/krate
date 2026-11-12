package sdk

// Process and automation ids: ports (channels between windows, views and
// workers), workers, offscreen frames, automation and extensions.
func init() {
	IDs = append(IDs,
		ID{"krate/port.channel", "port", "[]", "PortEnds", "port"},
		ID{"krate/port.transfer", "port", "[port: string, to: string]", "void", "port"},
		ID{"krate/port.post", "port:receive", "[port: string, data: unknown]", "void", "port"},
		ID{"krate/port.listen", "port:receive", "[port: string] (stream)", "PortEvent", "port"},
		ID{"krate/port.close", "port:receive", "[port: string]", "void", "port"},

		ID{"krate/worker.start", "worker", "[name: string, options?: WorkerStartOptions]", "string", "worker"},
		ID{"krate/worker.stop", "worker", "[id: string]", "void", "worker"},
		ID{"krate/worker.list", "worker", "[]", "WorkerInfo[]", "worker"},
		ID{"krate/worker.specs", "worker", "[]", "WorkerSpec[]", "worker"},
		ID{"krate/worker.post", "worker", "[id: string, data: unknown]", "void", "worker"},
		ID{"krate/worker.events", "worker", "[id?: string] (stream)", "WorkerEvent", "worker"},
		ID{"krate/worker.emit", "", "[data: unknown]", "void", "worker-host"},

		ID{"krate/offscreen.open", "offscreen", "[options?: OffscreenOptions]", "string", "offscreen"},
		ID{"krate/offscreen.capture", "offscreen", "[window: string, options?: CaptureOptions]", "Frame", "offscreen"},
		ID{"krate/offscreen.frames", "offscreen", "[window: string, options?: CaptureOptions] (stream)", "Frame", "offscreen"},

		ID{"krate/automation.endpoint", "automation", "[]", "AutomationEndpoint", "automation"},
		ID{"krate/automation.cdp", "automation", "[method: string, params?: object, view?: string]", "unknown", "automation"},
		ID{"krate/automation.cdpEvents", "automation", "[event: string, view?: string] (stream)", "unknown", "automation"},

		ID{"krate/extension.declared", "extension", "[]", "string[]", "extension"},
		ID{"krate/extension.list", "extension", "[]", "ExtensionInfo[]", "extension"},
		ID{"krate/extension.load", "extension", "[name: string]", "ExtensionInfo", "extension"},
		ID{"krate/extension.remove", "extension", "[id: string]", "void", "extension"},
	)
}
