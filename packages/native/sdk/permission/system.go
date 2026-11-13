package permission

func init() {
	for _, i := range []Info{
		{Name: "badge", Doc: "badge the app icon and show progress on it", Default: true},
		{Name: "screen", Doc: "list displays, find a window's display, center windows, display change events", Default: true},
		{Name: "power", Doc: "read power and idle state, receive suspend, resume, lock and battery events", Default: true},
		{Name: "power:inhibit", Doc: "keep the system or display awake"},
		{Name: "login", Doc: "read and change whether the app starts at login"},
		{Name: "theme", Doc: "read the system appearance, follow its changes, and force a light or dark title bar", Default: true},
		{Name: "crash", Doc: "list, read and delete crash reports, and receive crash events"},
	} {
		Define(i)
	}
}
