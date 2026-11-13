package app

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Host function ids of the open-file events.
const (
	IDOpenedFiles    = "krate/app.openedFiles"
	IDOpenFileEvents = "krate/app.openFileEvents"
	maxOpenedFiles   = 4096
	sourceLaunch     = "launch"
	sourceSecond     = "second-instance"
	sourceSystem     = "system"
)

// Sources of an OpenFileEvent.
const (
	// OpenedAtLaunch: the command line of this process.
	OpenedAtLaunch = sourceLaunch
	// OpenedBySecondInstance: the command line of a later launch handed to
	// this instance by SingleInstance.
	OpenedBySecondInstance = sourceSecond
	// OpenedBySystem: the operating system asked the running app to open the
	// file (macOS: Finder, "open -a", a drop on the Dock icon).
	OpenedBySystem = sourceSystem
)

// OpenFileEvent reports files the user asked the app to open.
type OpenFileEvent struct {
	// Paths are absolute paths of existing files.
	Paths []string
	// Index is the position of Paths[0] in the list OpenedFiles returns, so a
	// listener can tell what it has seen.
	Index int
	// Source says where the request came from: OpenedAtLaunch,
	// OpenedBySecondInstance or OpenedBySystem.
	Source string
}

// FilesFromArgs returns the existing files named by command line arguments,
// as absolute paths. A relative path is resolved against dir (the current
// directory when empty). Flags (arguments that start with "-"), URLs of other
// schemes and anything that is not a regular file are left out; a file://
// URL, which is how a Linux launcher passes %U, is converted to its path.
// Each path appears once.
func FilesFromArgs(args []string, dir string) []string {
	var out []string
	for _, a := range args {
		p, ok := argPath(a, dir)
		if !ok || slices.Contains(out, p) {
			continue
		}
		if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
			continue
		}
		out = append(out, p)
	}
	return out
}

func argPath(a, dir string) (string, bool) {
	if a == "" || a[0] == '-' {
		return "", false
	}
	if strings.HasPrefix(strings.ToLower(a), "file://") {
		u, err := url.Parse(a)
		if err != nil || (u.Host != "" && u.Host != "localhost") {
			return "", false
		}
		a = u.Path
		// file:///C:/dir/x.txt carries a slash before the drive letter.
		if len(a) >= 3 && a[0] == '/' && a[2] == ':' && runtime.GOOS == "windows" {
			a = a[1:]
		}
	} else if i := strings.Index(a, "://"); i > 0 && !strings.ContainsAny(a[:i], `/\`) {
		return "", false
	}
	a = filepath.FromSlash(a)
	if !filepath.IsAbs(a) {
		if dir == "" {
			d, err := os.Getwd()
			if err != nil {
				return "", false
			}
			dir = d
		}
		a = filepath.Join(dir, a)
	}
	return filepath.Clean(a), true
}

// launchArgs are the command line arguments whose files the app opens at
// start. Tests replace it.
var launchArgs = func() []string { return os.Args[1:] }

// CurrentLaunchArgs returns the command line arguments of this launch, without
// the program name: what LaunchArgs reports and SingleInstance passes on.
func CurrentLaunchArgs() []string { return launchArgs() }

// SetLaunchArgsForTest replaces the arguments read for the launch files and
// returns a function that restores them.
func SetLaunchArgsForTest(args []string) (restore func()) {
	old := launchArgs
	launchArgs = func() []string { return args }
	return func() { launchArgs = old }
}

// openStore keeps the files the app was asked to open and publishes them.
type openStore struct {
	mu    sync.Mutex
	list  []string
	topic *bridge.Topic
	// grant is called for every path before it is announced: a file the user
	// opened through the operating system is theirs to hand to the app, like
	// one chosen in a dialog.
	grant func(path string)
	// onOpen runs after a batch was announced, for example to raise the window.
	onOpen func(source string)
}

func (s *openStore) add(source string, paths []string) {
	if len(paths) == 0 {
		return
	}
	s.mu.Lock()
	room := maxOpenedFiles - len(s.list)
	if room <= 0 {
		s.mu.Unlock()
		return
	}
	if len(paths) > room {
		paths = paths[:room]
	}
	index := len(s.list)
	s.list = append(s.list, paths...)
	s.mu.Unlock()
	if s.grant != nil {
		for _, p := range paths {
			s.grant(p)
		}
	}
	s.topic.Publish(func(e *wire.Encoder) { encodeOpenEvent(e, OpenFileEvent{Paths: paths, Index: index, Source: source}) })
	if s.onOpen != nil {
		s.onOpen(source)
	}
}

func (s *openStore) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list)
}

func encodeOpenEvent(e *wire.Encoder, ev OpenFileEvent) {
	e.BeginObject()
	e.Key("paths")
	codec.WriteStrings(e, ev.Paths)
	e.Key("index")
	e.Int(int64(ev.Index))
	e.Key("source")
	e.String(ev.Source)
	e.EndObject()
}

func decodeOpenEvent(d *wire.Decoder) (ev OpenFileEvent, err error) {
	r := &codec.R{D: d, Name: "open file event"}
	r.Object(func(k string) bool {
		switch k {
		case "paths":
			ev.Paths = r.Strings()
		case "index":
			ev.Index = int(r.Int())
		case "source":
			ev.Source = r.String()
		default:
			return false
		}
		return true
	})
	return ev, r.Err()
}

// OpenedFiles returns every file the app was asked to open since it started:
// those on its command line first, then the ones later launches and the
// operating system passed on. The paths are absolute and, when the app has the
// fs permission, readable through it (see sdk/fs).
func OpenedFiles() ([]string, error) {
	d, err := bridge.Call(context.Background(), IDOpenedFiles, nil)
	if err != nil {
		return nil, err
	}
	return codec.ReadStrings(d)
}

type openListener struct {
	mu        sync.Mutex
	fn        func(string)
	delivered int
}

func (l *openListener) deliver(index int, paths []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, p := range paths {
		if index+i >= l.delivered {
			l.delivered = index + i + 1
			l.fn(p)
		}
	}
}

var (
	openMu    sync.Mutex
	openLs    []*openListener
	openCancl func()
	openEp    int64
)

func ensureOpenSub() error {
	if openCancl != nil && openEp == bridge.Epoch() {
		return nil
	}
	if openCancl != nil {
		openCancl()
		openCancl = nil
	}
	openEp = bridge.Epoch()
	c, err := bridge.Subscribe(context.Background(), IDOpenFileEvents, func(d *wire.Decoder) {
		ev, err := decodeOpenEvent(d)
		if err != nil {
			return
		}
		openMu.Lock()
		ls := slices.Clone(openLs)
		openMu.Unlock()
		for _, l := range ls {
			l.deliver(ev.Index, ev.Paths)
		}
	})
	if err != nil {
		return err
	}
	openCancl = c
	return nil
}

// OnOpenFile calls fn with the path of every file the app is asked to open:
// first the ones already opened (the command line files of the first launch
// among them), then each new one, from any goroutine, once each.
func OnOpenFile(fn func(path string)) error {
	openMu.Lock()
	if err := ensureOpenSub(); err != nil {
		openMu.Unlock()
		return err
	}
	l := &openListener{fn: fn}
	openLs = append(openLs, l)
	openMu.Unlock()
	files, err := OpenedFiles()
	if err != nil {
		return err
	}
	l.deliver(0, files)
	return nil
}

// registerOpenFiles adds the open-file operations to t and feeds the store
// from the command line and from the platform. It returns the store, which
// the single-instance handler also feeds.
func registerOpenFiles(t *bridge.Table, env *bridge.Env, topic *bridge.Topic, grant func(string), raise func()) *openStore {
	s := &openStore{topic: topic, grant: grant}
	s.onOpen = func(string) {
		if raise != nil {
			raise()
		}
	}
	t.Func(IDOpenedFiles, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDOpenedFiles).Done(); err != nil {
			return err
		}
		codec.WriteStrings(out, s.all())
		return nil
	})
	t.Stream(IDOpenFileEvents, Permission, true, topic.Stream())
	// Files on the command line are known now; the first page asks for them
	// with OpenedFiles.
	s.mu.Lock()
	if files := FilesFromArgs(launchArgs(), ""); len(files) > 0 {
		s.list = append(s.list, files...)
		for _, p := range files {
			if grant != nil {
				grant(p)
			}
		}
	}
	s.mu.Unlock()
	if of, ok := env.App.(caps.OpenFiles); ok {
		of.OnOpenFiles(func(paths []string) { s.add(sourceSystem, FilesFromArgs(paths, "")) })
	}
	return s
}
