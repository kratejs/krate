package sdk

// The remote area: web pages in views and windows that have no access to the
// app. Opening views, navigating and the message channel need "remote",
// request rules and the request stream "remote:intercept", and the user agent,
// proxy, certificates, logins and stored data "remote:network".
func init() {
	IDs = append(IDs,
		ID{"krate/remote.open", "remote", "[options: RemoteWindowOptions]", "string", "remote"},
		ID{"krate/remote.addView", "remote", "[window: string, options: RemoteViewOptions]", "string", "remote"},
		ID{"krate/remote.navigate", "remote", "[view: string, url: string]", "void", "remote"},
		ID{"krate/remote.post", "remote", "[view: string, targetOrigin: string, data: unknown]", "void", "remote"},
		ID{"krate/remote.messages", "remote", "[] (stream)", "RemoteMessage", "remote"},
		ID{"krate/remote.events", "remote", "[] (stream)", "RemoteEvent", "remote"},
		ID{"krate/remote.capabilities", "remote", "[]", "RemoteCapabilities", "remote"},
		ID{"krate/remote.setRules", "remote:intercept", "[partition: string, rules: RemoteRule[]]", "void", "remote"},
		ID{"krate/remote.requests", "remote:intercept", "[] (stream)", "RemoteRequest", "remote"},
		ID{"krate/remote.setUserAgent", "remote:network", "[partition: string, userAgent: string]", "void", "remote"},
		ID{"krate/remote.setProxy", "remote:network", "[partition: string, proxy: RemoteProxy]", "void", "remote"},
		ID{"krate/remote.setClientCertificate", "remote:network", "[partition: string, host: string, certFile: string, keyFile: string]", "void", "remote"},
		ID{"krate/remote.clearData", "remote:network", "[partition: string, options?: RemoteClearOptions]", "void", "remote"},
		ID{"krate/remote.certificates", "remote:network", "[] (stream)", "RemoteCertificateError", "remote"},
		ID{"krate/remote.answerCertificate", "remote:network", "[id: number, allow: boolean]", "void", "remote"},
		ID{"krate/remote.auth", "remote:network", "[] (stream)", "RemoteAuthChallenge", "remote"},
		ID{"krate/remote.answerAuth", "remote:network", "[id: number, user: string, password: string, ok: boolean]", "void", "remote"},
	)
}
