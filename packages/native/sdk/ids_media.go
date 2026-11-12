package sdk

// The media area: camera, microphone, screen capture and location requests of
// the page. Reading the state needs "media"; changing it, answering requests
// and listing the desktop's screens and windows need "media:manage".
func init() {
	IDs = append(IDs,
		ID{"krate/media.policy", "media", "[]", "MediaPolicy", "media"},
		ID{"krate/media.setPolicy", "media:manage", "[kind: PermissionKind, policy: PermissionPolicy]", "void", "media"},
		ID{"krate/media.decisions", "media", "[]", "PermissionDecision[]", "media"},
		ID{"krate/media.decide", "media:manage", "[kind: PermissionKind, scope: string, allow: boolean]", "void", "media"},
		ID{"krate/media.reset", "media:manage", "[kind?: PermissionKind]", "void", "media"},
		ID{"krate/media.requests", "media:manage", "[] (stream)", "PermissionRequest", "media"},
		ID{"krate/media.respond", "media:manage", "[id: number, allow: boolean, remember?: Remember]", "void", "media"},
		ID{"krate/media.events", "media:manage", "[] (stream)", "PermissionOutcome", "media"},
		ID{"krate/media.osStatus", "media", "[kind: OSConsentKind]", "OSConsent", "media"},
		ID{"krate/media.osRequest", "media:manage", "[kind: OSConsentKind]", "OSConsent", "media"},
		ID{"krate/media.sources", "media:manage", "[options?: {thumbnailWidth?: number}]", "CaptureSource[]", "media"},
		ID{"krate/media.chooseSource", "media:manage", "[id: string]", "void", "media"},
	)
}
