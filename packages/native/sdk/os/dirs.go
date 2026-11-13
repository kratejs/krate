package sdkos

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Dirs are the per-app directories.
type Dirs struct {
	Data, Cache, Config, Log, Temp string
}

// dirInput carries everything computeDirs reads from the environment, so each
// OS convention is testable on any host.
type dirInput struct {
	GOOS    string
	Getenv  func(string) string
	Home    string
	TempDir string
	UID     int
	AppID   string
}

func join(goos string, parts ...string) string {
	if goos == "windows" {
		return strings.Join(parts, `\`)
	}
	return strings.Join(parts, "/")
}

// absEnv returns the value of an environment variable only when it is an
// absolute path, as the XDG specification requires.
func absEnv(in dirInput, name string) string {
	v := in.Getenv(name)
	if v == "" {
		return ""
	}
	if in.GOOS == "windows" {
		if len(v) >= 3 && v[1] == ':' && (v[2] == '\\' || v[2] == '/') {
			return v
		}
		return ""
	}
	if v[0] == '/' {
		return v
	}
	return ""
}

// computeDirs applies each OS's convention.
//
//	Linux    data   $XDG_DATA_HOME/<id>     (default ~/.local/share)
//	         cache  $XDG_CACHE_HOME/<id>    (default ~/.cache)
//	         config $XDG_CONFIG_HOME/<id>   (default ~/.config)
//	         log    $XDG_STATE_HOME/<id>/log (default ~/.local/state)
//	         temp   $XDG_RUNTIME_DIR/<id>, else <tmp>/<id>-<uid>
//	macOS    data   ~/Library/Application Support/<id>
//	         cache  ~/Library/Caches/<id>
//	         config ~/Library/Application Support/<id>/config
//	         log    ~/Library/Logs/<id>
//	         temp   <tmp>/<id>
//	Windows  data   %APPDATA%\<id>
//	         cache  %LOCALAPPDATA%\<id>\Cache
//	         config %APPDATA%\<id>\Config
//	         log    %LOCALAPPDATA%\<id>\Logs
//	         temp   <tmp>\<id>
func computeDirs(in dirInput) (Dirs, error) {
	g, id := in.GOOS, in.AppID
	need := func(what string) error {
		return fmt.Errorf("sdkos: cannot find the %s directory (is HOME set?)", what)
	}
	switch g {
	case "windows":
		roaming, local := absEnv(in, "APPDATA"), absEnv(in, "LOCALAPPDATA")
		if roaming == "" || local == "" {
			return Dirs{}, errors.New("sdkos: %APPDATA% and %LOCALAPPDATA% are not set")
		}
		return Dirs{
			Data:   join(g, roaming, id),
			Cache:  join(g, local, id, "Cache"),
			Config: join(g, roaming, id, "Config"),
			Log:    join(g, local, id, "Logs"),
			Temp:   join(g, in.TempDir, id),
		}, nil
	case "darwin":
		if in.Home == "" {
			return Dirs{}, need("home")
		}
		lib := join(g, in.Home, "Library")
		support := join(g, lib, "Application Support", id)
		return Dirs{
			Data:   support,
			Cache:  join(g, lib, "Caches", id),
			Config: join(g, support, "config"),
			Log:    join(g, lib, "Logs", id),
			Temp:   join(g, in.TempDir, id),
		}, nil
	}
	base := func(env, rel string) (string, error) {
		if v := absEnv(in, env); v != "" {
			return v, nil
		}
		if in.Home == "" {
			return "", need(env)
		}
		return join(g, in.Home, rel), nil
	}
	data, err := base("XDG_DATA_HOME", ".local/share")
	if err != nil {
		return Dirs{}, err
	}
	cache, err := base("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return Dirs{}, err
	}
	config, err := base("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return Dirs{}, err
	}
	state, err := base("XDG_STATE_HOME", ".local/state")
	if err != nil {
		return Dirs{}, err
	}
	temp := join(g, in.TempDir, fmt.Sprintf("%s-%d", id, in.UID))
	if rt := absEnv(in, "XDG_RUNTIME_DIR"); rt != "" {
		temp = join(g, rt, id)
	}
	return Dirs{
		Data:   join(g, data, id),
		Cache:  join(g, cache, id),
		Config: join(g, config, id),
		Log:    join(g, state, id, "log"),
		Temp:   temp,
	}, nil
}

func systemDirs(id string) (Dirs, error) {
	home, _ := os.UserHomeDir()
	return computeDirs(dirInput{
		GOOS: runtime.GOOS, Getenv: os.Getenv, Home: home,
		TempDir: os.TempDir(), UID: os.Getuid(), AppID: id,
	})
}

func ensure(pick func(Dirs) string) (string, error) {
	a, err := Current()
	if err != nil {
		return "", err
	}
	d, err := systemDirs(a.ID)
	if err != nil {
		return "", err
	}
	p := filepath.FromSlash(pick(d))
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", fmt.Errorf("sdkos: creating %s: %w", p, err)
	}
	if err := checkPrivate(p); err != nil {
		return "", err
	}
	return p, nil
}

// AppDataDir is the directory for the app's persistent data. It is created on
// first use with mode 0700.
func AppDataDir() (string, error) { return ensure(func(d Dirs) string { return d.Data }) }

// CacheDir is the directory for data that can be regenerated.
func CacheDir() (string, error) { return ensure(func(d Dirs) string { return d.Cache }) }

// ConfigDir is the directory for user-editable configuration.
func ConfigDir() (string, error) { return ensure(func(d Dirs) string { return d.Config }) }

// LogDir is the directory for log files.
func LogDir() (string, error) { return ensure(func(d Dirs) string { return d.Log }) }

// TempDir is a per-app temporary directory. It is not cleaned automatically.
func TempDir() (string, error) { return ensure(func(d Dirs) string { return d.Temp }) }

// AllDirs returns every app directory (created on demand) for the fs scope.
func AllDirs() ([]string, error) {
	var out []string
	for _, f := range []func() (string, error){AppDataDir, CacheDir, ConfigDir, LogDir, TempDir} {
		p, err := f()
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
