// Package sdkupdater is the updater area of the Krate Native SDK: it exposes
// the update flow of package updater to the UI as krate/updater.* ids under
// the "updater" permission.
//
// The flow is check, download (a progress stream), apply, relaunch. The feed
// URL and the public key come from native.updater in the app configuration,
// through app.json. Go host code can call package updater directly.
package sdkupdater

import (
	"context"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/glue"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/updater"
	"github.com/kratejs/krate/packages/native/wire"
)

// Config is the native.updater block.
type Config struct {
	URL       string
	PublicKey string
}

var (
	mu      sync.Mutex
	cfg     Config
	pending *updater.Update
	busy    bool
	quit    func()

	// allowHTTP lets tests use an httptest server.
	allowHTTP bool
)

func init() {
	krate.RegisterCapability("updater", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return cfg.URL != "" && cfg.PublicKey != ""
	})
}

// Configure sets the feed for this process and removes what an earlier update
// left next to the executable.
func Configure(c Config) {
	mu.Lock()
	cfg = c
	pending = nil
	mu.Unlock()
	if c.URL != "" {
		go updater.CleanupStale("")
	}
}

// SetQuit sets the function relaunch calls to end the running app after the
// new process has started. Without it the process exits.
func SetQuit(f func()) {
	mu.Lock()
	quit = f
	mu.Unlock()
}

func current() (updater.Config, error) {
	mu.Lock()
	c := cfg
	mu.Unlock()
	if c.URL == "" || c.PublicKey == "" {
		return updater.Config{}, krate.Errorf("unsupported", "updates are not configured: set native.updater.url and native.updater.publicKey")
	}
	app, err := sdkos.Current()
	if err != nil {
		return updater.Config{}, err
	}
	cache, err := sdkos.CacheDir()
	if err != nil {
		return updater.Config{}, err
	}
	return updater.Config{URL: c.URL, PublicKey: c.PublicKey, Version: app.Version, CacheDir: cache, Insecure: allowHTTP}, nil
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*krate.Error); ok {
		return err
	}
	code := "internal"
	switch {
	case err == updater.ErrBadSignature:
		code = "bad_signature"
	case err == updater.ErrNotDownloaded:
		code = "not_downloaded"
	case err == updater.ErrNotConfigured:
		code = "unsupported"
	}
	return krate.Errorf(code, "%v", err)
}

// Check asks the feed and remembers the answer for Download and Apply.
func Check(ctx context.Context) (*updater.Update, error) {
	c, err := current()
	if err != nil {
		return nil, err
	}
	u, err := updater.Check(ctx, c)
	if err != nil {
		return nil, wrap(err)
	}
	mu.Lock()
	pending = u
	mu.Unlock()
	return u, nil
}

func take() (*updater.Update, error) {
	mu.Lock()
	defer mu.Unlock()
	if pending == nil {
		return nil, krate.Errorf("no_update", "no update is pending: call check first")
	}
	return pending, nil
}

// Register exposes the updater to the UI as krate/updater.* ids, gated by set.
func Register(r *scheme.Registry, set permission.Set) {
	const (
		idCheck    = "krate/updater.check"
		idDownload = "krate/updater.download"
		idApply    = "krate/updater.apply"
		idRelaunch = "krate/updater.relaunch"
	)
	noArgs := func(id string, args *wire.Decoder) error {
		a, err := glue.Open(id, args)
		if err != nil {
			return err
		}
		return a.Done()
	}

	permission.Register(r, set, idCheck, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := noArgs(idCheck, args); err != nil {
			return err
		}
		u, err := Check(ctx)
		if err != nil {
			return err
		}
		if u == nil {
			out.Null()
			return nil
		}
		out.BeginObject()
		out.Key("version")
		out.String(u.Version)
		out.Key("notes")
		out.String(u.Notes)
		out.Key("pubDate")
		out.String(u.PubDate)
		out.Key("size")
		out.Int(u.Size)
		out.EndObject()
		return nil
	})

	if !set.Allows("updater") {
		r.Stream(idDownload, func(krate.Ctx, *wire.Decoder, func(func(*wire.Encoder)) error) error {
			return permission.Denied("updater")
		})
	} else {
		r.Stream(idDownload, func(ctx krate.Ctx, args *wire.Decoder, emit func(write func(*wire.Encoder)) error) error {
			if err := noArgs(idDownload, args); err != nil {
				return err
			}
			u, err := take()
			if err != nil {
				return err
			}
			mu.Lock()
			if busy {
				mu.Unlock()
				return krate.Errorf("busy", "a download is already running")
			}
			busy = true
			mu.Unlock()
			defer func() { mu.Lock(); busy = false; mu.Unlock() }()

			var last time.Time
			var emitErr error
			send := func(done, total int64) {
				if emitErr != nil {
					return
				}
				// At most ten events a second, and always the last.
				if now := time.Now(); done != total && now.Sub(last) < 100*time.Millisecond {
					return
				} else {
					last = now
				}
				emitErr = emit(func(e *wire.Encoder) {
					e.BeginObject()
					e.Key("done")
					e.Int(done)
					e.Key("total")
					e.Int(total)
					e.EndObject()
				})
			}
			if err := u.Download(ctx, send); err != nil {
				return wrap(err)
			}
			return emitErr
		})
	}

	permission.Register(r, set, idApply, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := noArgs(idApply, args); err != nil {
			return err
		}
		u, err := take()
		if err != nil {
			return err
		}
		if err := u.Apply(); err != nil {
			return wrap(err)
		}
		out.Null()
		return nil
	})

	permission.Register(r, set, idRelaunch, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := noArgs(idRelaunch, args); err != nil {
			return err
		}
		if err := updater.Relaunch(); err != nil {
			return wrap(err)
		}
		out.Null()
		mu.Lock()
		q := quit
		mu.Unlock()
		go func() {
			// Let the reply reach the page before the process ends.
			time.Sleep(150 * time.Millisecond)
			if q != nil {
				q()
				return
			}
			exit(0)
		}()
		return nil
	})
}
