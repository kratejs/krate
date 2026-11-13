package permission

func init() {
	for _, i := range []Info{
		{Name: "port", Doc: "create message channels and hand their ends to windows, views and workers", Implies: []string{"port:receive"}},
		{Name: "port:receive", Doc: "be handed a channel end and post to, listen on and close the ends it holds"},
		{Name: "worker", Doc: "start, stop and message the background worker processes the app declares in native.workers"},
		{Name: "offscreen", Doc: "open windows that render out of sight and capture frames of windows and views"},
		{Name: "automation", Doc: "read the remote debugging endpoint and send DevTools protocol commands (only when the app runs in dev or with native.debug)"},
		{Name: "extension", Doc: "list, load and remove the browser extensions the app packages"},
	} {
		Define(i)
	}
}
