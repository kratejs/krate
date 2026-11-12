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
	)
}
