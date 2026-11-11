//go:build linux

// Command krate-shell-demo opens one window with a hello page. Manual smoke
// test for the platform layer.
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/kratejs/krate/packages/native/platform"
)

const page = `<!doctype html><meta charset=utf-8><title>Krate</title>
<body style="font:16px system-ui;margin:2rem">
<h1>Hello from Krate</h1>
<p>origin: <code id=o></code> secure: <code id=s></code></p>
<pre id=t></pre>
<script>
o.textContent = location.origin; s.textContent = isSecureContext;
(async () => {
  const r = await fetch("/ticks"); const rd = r.body.getReader(); const dec = new TextDecoder();
  for (;;) { const {done, value} = await rd.read(); if (done) break; t.textContent += dec.decode(value); }
})();
</script>`

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	})
	mux.HandleFunc("/ticks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for i := 1; i <= 5; i++ {
			fmt.Fprintf(w, "tick %d\n", i)
			w.(http.Flusher).Flush()
			select {
			case <-time.After(500 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
	})
	app, err := platform.New(platform.Options{AppID: "dev.krate.demo", Handler: mux, Debug: true})
	if err != nil {
		log.Fatal(err)
	}
	err = app.Run(func() {
		if _, err := app.NewWindow(platform.WindowOptions{Title: "Krate demo", Width: 800, Height: 600, Resizable: true, Path: "/"}); err != nil {
			log.Print(err)
			app.Quit()
		}
	})
	if err != nil {
		log.Fatal(err)
	}
}
