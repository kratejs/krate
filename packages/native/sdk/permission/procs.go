package permission

func init() {
	for _, i := range []Info{
		{Name: "port", Doc: "create message channels and hand their ends to windows, views and workers", Implies: []string{"port:receive"}},
		{Name: "port:receive", Doc: "be handed a channel end and post to, listen on and close the ends it holds"},
		{Name: "worker", Doc: "start, stop and message the background worker processes the app declares in native.workers"},
	} {
		Define(i)
	}
}
