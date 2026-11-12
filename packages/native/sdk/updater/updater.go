// Package sdkupdater is the updater area of the Krate Native SDK: it exposes
// the update flow of package updater to the UI as krate/updater.* ids under
// the "updater" permission.
//
// The flow is check, download (a progress stream), apply, relaunch. The feed
// URL and the public key come from native.updater in the app configuration,
// through app.json. Go host code can call package updater directly.
//
// After an applied update the new version has to confirm a healthy start:
// the shell does so a few seconds after the first page was served, or the app
// calls markHealthy (native.updater.confirm "app"). A version that crashes or
// does not confirm is rolled back at the next launch (see Boot).
package sdkupdater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/distinfo"
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
	// Channel fills {channel} in URL; default "stable".
	Channel string
	// MaxStarts is how many starts a new version gets to confirm before the
	// next launch restores the previous one; default 1.
	MaxStarts int
	// HealthTimeout is the seconds a start may take to confirm; default 60.
	HealthTimeout int
	// ManualConfirm switches off the shell's automatic confirmation: the app
	// calls markHealthy itself.
	ManualConfirm bool
}

// GraceEnv names an environment variable that sets the milliseconds between
// the first page being served and the automatic confirmation (default 3000).
// CI and tests use it.
const GraceEnv = "KRATE_UPDATER_HEALTH_GRACE_MS"

// NoRelaunchEnv, when "1", makes a rollback restore the files without
// starting the previous version (CI uses it to inspect the result).
const NoRelaunchEnv = "KRATE_UPDATER_ROLLBACK_NO_RELAUNCH"

var (
	mu      sync.Mutex
	cfg     Config
	pending *updater.Update
	busy    bool
	quit    func()
	guard   *updater.Guard
	served  bool

	// allowHTTP lets tests use an httptest server.
	allowHTTP bool
)

func init() {
	krate.RegisterCapability("updater", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return cfg.URL != "" && cfg.PublicKey != "" && distinfo.Current().SelfUpdate()
	})
}

// Configure sets the feed for this process and removes what an earlier update
// left next to the executable.
func Configure(c Config) {
	mu.Lock()
	cfg = c
	pending = nil
	guard = nil
	served = false
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
	if d := distinfo.Current(); !d.SelfUpdate() {
		return updater.Config{}, krate.Errorf("unsupported", "updates are not available here: %s", d.UpdateNote())
	}
	app, err := sdkos.Current()
	if err != nil {
		return updater.Config{}, err
	}
	cache, err := sdkos.CacheDir()
	if err != nil {
		return updater.Config{}, err
	}
	state, err := stateDir()
	if err != nil {
		return updater.Config{}, err
	}
	return updater.Config{
		URL: c.URL, PublicKey: c.PublicKey, AppID: app.ID, Version: app.Version, CacheDir: cache, Insecure: allowHTTP,
		Channel: c.Channel, StateDir: state, Target: distinfo.Current().Target,
		Logf: func(f string, a ...any) { fmt.Fprintf(os.Stderr, "krate: "+f+"\n", a...) },
	}, nil
}

// stateDir is where the install id, the rollback state and the chosen
// channel live: <app data dir>/updater.
func stateDir() (string, error) {
	d, err := sdkos.AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "updater"), nil
}

// Boot runs the first-start protection of an updated app; the shell calls it
// right after the SDK is configured. When the previous start of a freshly
// updated version never confirmed, Boot restores the previous version, starts
// it and exits this process. Otherwise it arms the watchdog of an unconfirmed
// start. logf may be nil.
func Boot(logf func(format string, args ...any)) {
	mu.Lock()
	c := cfg
	mu.Unlock()
	if c.URL == "" || c.PublicKey == "" || !distinfo.Current().SelfUpdate() {
		return
	}
	app, err := sdkos.Current()
	if err != nil {
		return
	}
	dir, err := stateDir()
	if err != nil {
		return
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	res, err := updater.Boot(updater.BootConfig{
		StateDir: dir, Version: app.Version, MaxStarts: c.MaxStarts,
		Timeout: time.Duration(c.HealthTimeout) * time.Second, Logf: logf,
	})
	if err != nil {
		logf("updater: %v", err)
	}
	if res.RolledBack != nil {
		if os.Getenv(NoRelaunchEnv) != "1" && res.Restart != "" {
			if err := updater.RelaunchPath(res.Restart); err != nil {
				logf("updater: %v", err)
				return
			}
		}
		exit(0)
		return
	}
	mu.Lock()
	guard = res.Guard
	mu.Unlock()
}

// PageServed tells the updater the app's first page was requested. Unless the
// app confirms by itself it is marked healthy after a short grace period, so
// a crash right after the window opened still counts as a failed start.
func PageServed() {
	mu.Lock()
	if served || guard == nil || cfg.ManualConfirm {
		mu.Unlock()
		return
	}
	served = true
	mu.Unlock()
	grace := 3 * time.Second
	if v, err := strconv.Atoi(os.Getenv(GraceEnv)); err == nil && v >= 0 {
		grace = time.Duration(v) * time.Millisecond
	}
	time.AfterFunc(grace, func() { _ = MarkHealthy() })
}

// CleanExit tells the updater the app is quitting normally. If its first page
// was served, a quick quit counts as a healthy start.
func CleanExit() {
	mu.Lock()
	ok := served && !cfg.ManualConfirm
	mu.Unlock()
	if ok {
		_ = MarkHealthy()
	}
}

// MarkHealthy confirms that this version started fine: the previous version
// kept for rollback is deleted. Harmless when nothing is pending.
func MarkHealthy() error {
	mu.Lock()
	g := guard
	mu.Unlock()
	return g.MarkHealthy()
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
		idHealthy  = "krate/updater.markHealthy"
		idStatus   = "krate/updater.status"
		idAck      = "krate/updater.ackRollback"
		idChannel  = "krate/updater.setChannel"
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
		out.Key("critical")
		out.Bool(u.Critical)
		out.Key("required")
		out.Bool(u.Required)
		out.Key("delta")
		out.Bool(u.HasDelta())
		out.Key("downloadSize")
		out.Int(u.DownloadSize())
		out.EndObject()
		return nil
	})

	if !set.Allows("updater") {
		r.Stream(idDownload, func(krate.Ctx, *wire.Decoder, func(func(*wire.Encoder)) error) error {
			return permission.Denied("updater")
		})
	} else {
		r.Stream(idDownload, func(ctx krate.Ctx, args *wire.Decoder, emit func(write func(*wire.Encoder)) error) error {
			if !set.AllowsFor(ctx.Window(), "updater") {
				return permission.Denied("updater")
			}
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

	permission.Register(r, set, idHealthy, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := noArgs(idHealthy, args); err != nil {
			return err
		}
		if err := MarkHealthy(); err != nil {
			return wrap(err)
		}
		out.Null()
		return nil
	})

	permission.Register(r, set, idStatus, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := noArgs(idStatus, args); err != nil {
			return err
		}
		c, err := current()
		if err != nil {
			return err
		}
		st := updater.LoadState(c.StateDir)
		out.BeginObject()
		out.Key("version")
		out.String(c.Version)
		out.Key("channel")
		out.String(effectiveChannel(c, st))
		out.Key("unconfirmed")
		out.Bool(st.Phase == updater.PhasePending && st.To == c.Version)
		out.Key("rollback")
		if ev := st.Event; ev != nil {
			out.BeginObject()
			out.Key("failed")
			out.String(ev.Failed)
			out.Key("restored")
			out.String(ev.Restored)
			out.Key("reason")
			out.String(ev.Reason)
			out.Key("at")
			out.String(ev.At.UTC().Format(time.RFC3339))
			out.EndObject()
		} else {
			out.Null()
		}
		out.EndObject()
		return nil
	})

	permission.Register(r, set, idAck, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := noArgs(idAck, args); err != nil {
			return err
		}
		c, err := current()
		if err != nil {
			return err
		}
		if err := updater.AckRollback(c.StateDir); err != nil {
			return wrap(err)
		}
		out.Null()
		return nil
	})

	permission.Register(r, set, idChannel, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		a, err := glue.Open(idChannel, args)
		if err != nil {
			return err
		}
		name, err := a.String("channel")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		c, err := current()
		if err != nil {
			return err
		}
		if err := updater.SetChannel(c.StateDir, name); err != nil {
			return krate.Errorf("bad_args", "%v", err)
		}
		mu.Lock()
		pending = nil
		mu.Unlock()
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

func effectiveChannel(c updater.Config, st updater.State) string {
	if st.Channel != "" {
		return st.Channel
	}
	if c.Channel != "" {
		return c.Channel
	}
	return updater.DefaultChannel
}
