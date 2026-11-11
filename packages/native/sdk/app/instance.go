package app

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var idRe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// socketPath returns the lock socket for appID inside dir (the user runtime
// directory when empty). Unix socket paths are short, so a long path falls
// back to a hash.
func socketPath(dir, appID string) (string, error) {
	if dir == "" {
		dir = os.Getenv("XDG_RUNTIME_DIR")
	}
	if dir == "" {
		c, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(c, "krate")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := "krate-" + idRe.ReplaceAllString(appID, "_") + ".sock"
	p := filepath.Join(dir, name)
	if len(p) > 100 {
		p = filepath.Join(dir, "krate-"+hash8(appID)+".sock")
	}
	if len(p) > 100 {
		p = filepath.Join(os.TempDir(), fmt.Sprintf("krate-%d-%s.sock", os.Getuid(), hash8(appID)))
	}
	return p, nil
}

func hash8(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// Instance is a held single-instance lock.
type Instance struct {
	ln   net.Listener
	path string
	once sync.Once
}

// Release gives up the lock.
func (i *Instance) Release() {
	i.once.Do(func() {
		i.ln.Close()
		os.Remove(i.path)
	})
}

type message struct {
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
}

// Acquire claims the lock for appID in dir ("" = the user runtime
// directory). When another instance holds it, args are delivered to that
// instance and Acquire returns (nil, false, nil). Otherwise it returns the
// held lock, and onSecond is called (on its own goroutine) for every later
// launch.
func Acquire(dir, appID string, args []string, onSecond func(SecondInstance)) (*Instance, bool, error) {
	path, err := socketPath(dir, appID)
	if err != nil {
		return nil, false, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		if err := send(path, args); err == nil {
			return nil, false, nil
		} else if !errors.Is(err, errNoServer) {
			return nil, false, err
		}
		// No live instance: remove a stale socket file and listen.
		os.Remove(path)
		ln, err := net.Listen("unix", path)
		if err != nil {
			// Lost a race with another starting instance; try to talk to it.
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if err := os.Chmod(path, 0o600); err != nil {
			_ = ln.Close()
			_ = os.Remove(path)
			return nil, false, fmt.Errorf("app: securing the single-instance socket %s: %w", path, err)
		}
		inst := &Instance{ln: ln, path: path}
		go inst.serve(onSecond)
		return inst, true, nil
	}
	return nil, false, fmt.Errorf("app: cannot claim the single-instance lock at %s", path)
}

var errNoServer = errors.New("no instance running")

func send(path string, args []string) error {
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return errNoServer
	}
	defer c.Close()
	if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	dir, _ := os.Getwd()
	b, _ := json.Marshal(message{Args: args, Dir: dir})
	if _, err := c.Write(append(b, '\n')); err != nil {
		return err
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil || line != "ok\n" {
		return fmt.Errorf("app: the running instance did not accept the launch: %v", err)
	}
	return nil
}

func (i *Instance) serve(onSecond func(SecondInstance)) {
	for {
		c, err := i.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			if c.SetDeadline(time.Now().Add(5*time.Second)) != nil {
				return
			}
			line, err := bufio.NewReaderSize(c, 1<<20).ReadBytes('\n')
			if err != nil {
				return
			}
			var m message
			if json.Unmarshal(line, &m) != nil {
				return
			}
			_, _ = c.Write([]byte("ok\n")) // the second instance exits either way
			if onSecond != nil {
				onSecond(SecondInstance{Args: m.Args, Dir: m.Dir, URLs: URLsFromArgs(m.Args)})
			}
		}()
	}
}
