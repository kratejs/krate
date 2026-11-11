package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/native/gohost"
)

// hostChild is one running JavaScript host child.
type hostChild struct {
	spec  HostChildSpec
	prog  string
	args  []string
	child *gohost.Child
}

// commandOf resolves the program and arguments that start spec.
func (a *app) commandOf(spec HostChildSpec) (prog string, args []string, err error) {
	entry := filepath.Join(a.dir, filepath.FromSlash(spec.Entry))
	if _, err := os.Stat(entry); err != nil {
		return "", nil, fmt.Errorf("shell: the %s host child: %w", spec.Runtime, err)
	}
	if len(spec.Command) == 0 {
		return entry, nil, nil
	}
	prog = spec.Command[0]
	if !strings.ContainsAny(prog, `/\`) {
		p, err := exec.LookPath(prog)
		if err != nil {
			return "", nil, fmt.Errorf("shell: the %s host child needs %q on PATH: %w (install it, or build the app with native.host.bundle so the runtime ships inside it)", spec.Runtime, prog, err)
		}
		prog = p
	}
	for _, s := range spec.Command[1:] {
		s = strings.ReplaceAll(s, "{entry}", entry)
		s = strings.ReplaceAll(s, "{sockdir}", sockDir())
		args = append(args, s)
	}
	return prog, args, nil
}

// sockDir is the directory the host child sockets live under, which a Deno
// child needs to read and write.
func sockDir() string {
	if c, err := os.UserCacheDir(); err == nil {
		return filepath.Join(c, "krate", "native")
	}
	return os.TempDir()
}

// restartHostChildren starts the host children the manifest lists, restarts
// the ones already running (a script is re-read from disk on every start) and
// stops those that are no longer listed. Their ids are registered on first
// start and again for ids that appeared later.
func (a *app) restartHostChildren() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	man, err := ReadManifest(a.dir)
	if err != nil {
		return err
	}
	a.man = man
	keep := map[string]bool{}
	for i, spec := range man.HostChildren {
		keep[spec.Entry] = true
		if err := a.startHostChild(i, spec, man); err != nil {
			return err
		}
	}
	for entry, hc := range a.hostChildren {
		if !keep[entry] {
			hc.child.Stop()
			delete(a.hostChildren, entry)
		}
	}
	return nil
}

func (a *app) startHostChild(index int, spec HostChildSpec, man *Manifest) error {
	prog, args, err := a.commandOf(spec)
	if err != nil {
		return err
	}
	hc := a.hostChildren[spec.Entry]
	if hc != nil && (hc.prog != prog || !slices.Equal(hc.args, args)) {
		return fmt.Errorf("shell: the command of the %s host child changed; restart the app", spec.Runtime)
	}
	if hc == nil {
		hc = &hostChild{spec: spec, prog: prog, args: args}
		hc.child = gohost.NewChild(gohost.ChildOptions{
			Logf:         func(format string, args ...any) { a.logf(format, args...) },
			Prefix:       "[" + spec.Runtime + "] ",
			Args:         args,
			Dir:          a.dir,
			Name:         "js" + strconv.Itoa(index),
			ReadyAnyLine: true,
			ReadyTimeout: 60 * time.Second,
		})
		parent, err := a.sdk.ServeParent()
		if err != nil {
			return err
		}
		env := []string{
			"KRATE_APP_DIR=" + a.dir,
			"KRATE_APP_ID=" + man.AppID,
			"KRATE_APP_NAME=" + man.ProductName,
			"KRATE_APP_VERSION=" + man.Version,
			gohost.ParentEnv + "=" + parent,
		}
		if a.opts.Dev {
			env = append(env, "KRATE_DEV=1", "NODE_ENV=development")
		} else {
			env = append(env, "NODE_ENV=production")
		}
		if err := hc.child.Start(context.Background(), prog, env); err != nil {
			return err
		}
		if a.hostChildren == nil {
			a.hostChildren = map[string]*hostChild{}
		}
		a.hostChildren[spec.Entry] = hc
	} else {
		hc.spec = spec
		if err := hc.child.Restart(prog); err != nil {
			return err
		}
	}
	funcs, streams, signals := spec.Funcs, spec.Streams, spec.Signals
	if l, err := listChild(hc.child); err != nil {
		a.logf("shell: the %s host child did not list its ids: %v", spec.Runtime, err)
	} else {
		funcs, streams, signals = union(funcs, l.Funcs), union(streams, l.Streams), union(signals, l.Signals)
	}
	return guard("registering "+spec.Runtime+" host child functions", func() {
		hc.child.Remote().RegisterInto(a.reg, funcs, streams, signals)
	})
}

type childListing struct {
	Funcs   []string `json:"funcs"`
	Streams []string `json:"streams"`
	Signals []string `json:"signals"`
}

// listChild asks a host child which ids it registered.
func listChild(c *gohost.Child) (childListing, error) {
	var l childListing
	client := c.Client()
	if client == nil {
		return l, errors.New("not running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := newGet(ctx, c.Base()+gohost.ListPath)
	if err != nil {
		return l, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return l, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return l, fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return l, err
	}
	return l, json.Unmarshal(b, &l)
}

func union(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, s := range b {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func newGet(ctx context.Context, url string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
}
