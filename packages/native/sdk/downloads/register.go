package downloads

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// maxFinished is how many finished downloads List remembers.
const maxFinished = 50

// tracker remembers the downloads for List.
type tracker struct {
	mu    sync.Mutex
	order []int64
	byID  map[int64]*Info
}

func (t *tracker) update(in Info) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byID == nil {
		t.byID = map[int64]*Info{}
	}
	cur := t.byID[in.ID]
	if cur == nil {
		cur = &Info{}
		t.byID[in.ID] = cur
		t.order = append(t.order, in.ID)
	}
	*cur = in
	// Forget the oldest finished downloads.
	finished := 0
	for _, id := range t.order {
		if s := t.byID[id].State; s == Completed || s == Cancelled || s == Failed {
			finished++
		}
	}
	for i := 0; finished > maxFinished && i < len(t.order); {
		id := t.order[i]
		if s := t.byID[id].State; s == Completed || s == Cancelled || s == Failed {
			delete(t.byID, id)
			t.order = append(t.order[:i], t.order[i+1:]...)
			finished--
			continue
		}
		i++
	}
}

func (t *tracker) list() []Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Info, 0, len(t.order))
	for _, id := range t.order {
		out = append(out, *t.byID[id])
	}
	return out
}

// policy is the destination policy.
type policy struct {
	mu  sync.Mutex
	cfg Options
}

func (p *policy) get() Options {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cfg
}

func (p *policy) set(o Options) {
	p.mu.Lock()
	p.cfg = o
	p.mu.Unlock()
}

// DefaultDir is where downloads go when nothing is configured and no dialog
// can ask: the user's Downloads folder, or the home folder.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	d := filepath.Join(home, "Downloads")
	if st, err := os.Stat(d); err == nil && st.IsDir() {
		return d
	}
	return home
}

// silentPath picks a free file name in dir, creating dir.
func silentPath(dir, suggested string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := caps.UniqueName(suggested, func(c string) bool {
		_, err := os.Lstat(filepath.Join(dir, c))
		return err == nil
	})
	return filepath.Join(dir, name), nil
}

// Register adds the operations to t and takes over the downloads of the
// platform.
func Register(t *bridge.Table, env *bridge.Env) {
	topic := env.Topic("downloads")
	t.Stream(IDEvents, Permission, true, topic.Stream())
	trk := &tracker{}
	pol := &policy{}
	dl, hasDL := env.App.(caps.Downloads)

	if hasDL {
		dl.OnDownload(func(e caps.DownloadEvent) {
			in := fromCaps(env, e)
			trk.update(in)
			topic.Publish(func(enc *wire.Encoder) { encodeInfo(enc, in) })
		})
		dl.OnDownloadStart(func(r caps.DownloadRequest, decide func(string)) {
			// The dialog blocks, and the platform handler must not.
			go func() { decide(chooseDestination(env, pol.get(), r)) }()
		})
	}
	unsupported := func() error {
		return krate.Errorf("unsupported", "downloads cannot be controlled on this platform")
	}

	t.Func(IDConfigure, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDConfigure)
		var o Options
		r.Object(func(k string) bool {
			switch k {
			case "dir":
				o.Dir = r.String()
			case "ask":
				o.Ask = r.Bool()
			default:
				return false
			}
			return true
		})
		if err := r.Done(); err != nil {
			return err
		}
		if o.Dir != "" && !filepath.IsAbs(o.Dir) {
			return krate.Errorf("bad_args", "%s: dir %q must be an absolute path", IDConfigure, o.Dir)
		}
		if !hasDL {
			return unsupported()
		}
		pol.set(o)
		out.Null()
		return nil
	})
	t.Func(IDList, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDList)
		if err := r.Done(); err != nil {
			return err
		}
		out.BeginArray()
		for _, in := range trk.list() {
			encodeInfo(out, in)
		}
		out.EndArray()
		return nil
	})
	t.Func(IDCancel, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDCancel)
		id := r.Int()
		if err := r.Done(); err != nil {
			return err
		}
		if !hasDL {
			return unsupported()
		}
		if id <= 0 {
			return krate.Errorf("bad_args", "%s: %d is not a download id", IDCancel, id)
		}
		if err := dl.CancelDownload(uint64(id)); err != nil {
			return err
		}
		out.Null()
		return nil
	})
}

// chooseDestination applies the policy to one download and returns the path
// to write to, or "" to cancel it.
func chooseDestination(env *bridge.Env, cfg Options, r caps.DownloadRequest) string {
	name := caps.SafeFileName(r.SuggestedName)
	dialogs, hasDialogs := env.App.(caps.Dialogs)
	if cfg.Dir != "" && !cfg.Ask || !hasDialogs {
		dir := cfg.Dir
		if dir == "" {
			dir = DefaultDir()
		}
		p, err := silentPath(dir, name)
		if err != nil {
			return ""
		}
		return p
	}
	dir := cfg.Dir
	if dir == "" {
		dir = DefaultDir()
	}
	var parent caps.WindowRef
	if rec, ok := env.Windows.ViewByRef(r.View); ok {
		parent = env.Windows.Ref(rec.Window)
	}
	paths, err := dialogs.FileDialog(parent, caps.FileDialogOptions{
		Mode: caps.SaveFile, Title: "Save " + name, Dir: dir, Name: name, ConfirmWrite: true,
	})
	if err != nil || len(paths) == 0 {
		return ""
	}
	return paths[0]
}
