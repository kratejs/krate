package sdk

// UIPermission names one app permission that gates UI access to an SDK area.
type UIPermission struct {
	Name        string
	Description string
}

// UIPermissions is the table of permissions the UI half of the SDK checks.
// It is kept separate from the data half's table so that the two can be
// merged into one permission package later. Go host code is never gated; only
// calls that arrive from the page are.
var UIPermissions = []UIPermission{
	{"dialog", "Show native file and message dialogs"},
	{"clipboard", "Write the system clipboard"},
	{"clipboard:read", "Read the system clipboard (sees what the user copied elsewhere)"},
	{"notify", "Post desktop notifications"},
	{"tray", "Show a system tray icon"},
	{"menu", "Set the application menu and show context menus"},
	{"shortcut", "Register global keyboard shortcuts"},
	{"window", "Open and control windows"},
	{"app", "Quit or relaunch the app, query capabilities, receive second-instance events"},
}

// IsUIPermission reports whether name is in UIPermissions.
func IsUIPermission(name string) bool {
	for _, p := range UIPermissions {
		if p.Name == name {
			return true
		}
	}
	return false
}
