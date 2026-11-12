package sdk

// OS integration ids: badge, screen, power, login, theme and crash.
func init() {
	IDs = append(IDs,
		ID{"krate/badge.set", "badge", "[text: string]", "void", "badge"},
		ID{"krate/badge.get", "badge", "[]", "string", "badge"},
		ID{"krate/badge.setProgress", "badge", "[state: ProgressState, value?: number]", "void", "badge"},
		ID{"krate/badge.progress", "badge", "[]", "BadgeProgress", "badge"},
		ID{"krate/badge.setUrgent", "badge", "[on: boolean]", "void", "badge"},

		ID{"krate/screen.list", "screen", "[]", "Display[]", "screen"},
		ID{"krate/screen.primary", "screen", "[]", "Display", "screen"},
		ID{"krate/screen.ofWindow", "screen", "[window?: string]", "Display", "screen"},
		ID{"krate/screen.centerWindow", "screen", "[window: string, display?: string]", "void", "screen"},
		ID{"krate/screen.events", "screen", "[] (stream)", "ScreenEvent", "screen"},

		ID{"krate/power.state", "power", "[]", "PowerState", "power"},
		ID{"krate/power.idleTime", "power", "[]", "number", "power"},
		ID{"krate/power.setIdleThreshold", "power", "[seconds: number]", "void", "power"},
		ID{"krate/power.preventSleep", "power:inhibit", "[reason: string, display?: boolean]", "string", "power"},
		ID{"krate/power.allowSleep", "power:inhibit", "[id: string]", "void", "power"},
		ID{"krate/power.events", "power", "[] (stream)", "PowerEvent", "power"},

		ID{"krate/login.get", "login", "[]", "LoginItem", "login"},
		ID{"krate/login.set", "login", "[enabled: boolean, options?: LoginOptions]", "void", "login"},

		ID{"krate/theme.get", "theme", "[]", "Theme", "theme"},
		ID{"krate/theme.set", "theme", "[source: ThemeSource]", "void", "theme"},
		ID{"krate/theme.events", "theme", "[] (stream)", "Theme", "theme"},

		ID{"krate/crash.list", "crash", "[]", "CrashSummary[]", "crash"},
		ID{"krate/crash.read", "crash", "[id: string]", "CrashReport", "crash"},
		ID{"krate/crash.delete", "crash", "[id: string]", "void", "crash"},
		ID{"krate/crash.setAutoReload", "crash", "[on: boolean]", "void", "crash"},
		ID{"krate/crash.events", "crash", "[] (stream)", "CrashEvent", "crash"},
	)
}
