package shell

import (
	"archive/zip"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"syscall"

	"github.com/kratejs/krate/packages/native/gohost"
	"github.com/kratejs/krate/packages/native/scheme"
)

// ProfileEnv names the environment variable that makes Main (and a Go host
// child) write a CPU profile to that path when the process exits. `krate
// native profile` uses it to collect a profile for profile-guided builds.
const ProfileEnv = gohost.ProfileEnv

// BuildInfo is "<product> <version>" of a release build, set by the build with
// -ldflags "-X github.com/kratejs/krate/packages/native/shell.BuildInfo=...".
var BuildInfo string

// RunEmbedded runs the app held in fsys, an app directory (see README.md)
// that a release build embeds with go:embed. Go host code is linked into the
// process and registered through opts.GoGlue; there is no child process.
// It must be called like Run (main goroutine, locked thread).
func RunEmbedded(fsys fs.FS, opts RunOptions) error {
	if opts.Logf == nil {
		opts.Logf = func(format string, args ...any) { fmt.Fprintf(os.Stderr, "krate: "+format+"\n", args...) }
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	a, err := loadApp("", appRoot(fsys), opts)
	if err != nil {
		return err
	}
	defer a.close()
	if opts.Browser {
		return a.runBrowser()
	}
	return a.runNative()
}

// ZipFS opens an embedded zip archive as a file system. A release build packs
// the app directory into one zip and embeds it with go:embed: go:embed rejects
// file names such as "tasks/[id]/index.html" that dynamic routes produce, and
// one archive keeps the executable's assets compressed.
func ZipFS(data []byte) (fs.FS, error) {
	return zip.NewReader(bytes.NewReader(data), int64(len(data)))
}

// MustZipFS is ZipFS for a main function: it exits the process when the
// archive is damaged.
func MustZipFS(data []byte) fs.FS {
	f, err := ZipFS(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "krate: the embedded app is damaged:", err)
		os.Exit(1)
	}
	return f
}

// appRoot returns the app directory inside an embedded file system: its "app"
// subdirectory when it has one, the file system itself otherwise.
func appRoot(fsys fs.FS) fs.FS {
	if _, err := fs.Stat(fsys, "app/"+ManifestFile); err == nil {
		if sub, err := fs.Sub(fsys, "app"); err == nil {
			return sub
		}
	}
	return fsys
}

// Main is the whole main function of a release build:
//
//	//go:embed app.zip
//	var appZip []byte
//
//	func init() { runtime.LockOSThread() }
//	func main() { shell.Main(shell.MustZipFS(appZip), glue.Register) }
//
// It parses the command line (--browser, --addr, --version), runs the app and
// exits the process. register, when not nil, registers the app's Go host code.
func Main(embedded fs.FS, register func(*scheme.Registry)) {
	// stdout belongs to the control protocol ("url ...", "ready"); anything
	// else that prints to os.Stdout lands on stderr.
	ctl := os.Stdout
	os.Stdout = os.Stderr
	os.Exit(RunMain(os.Args[1:], embedded, register, ctl))
}

// RunMain is Main without the process exit, for tests. ctl receives the
// protocol lines.
func RunMain(args []string, embedded fs.FS, register func(*scheme.Registry), ctl io.Writer) int {
	fl := flag.NewFlagSet("app", flag.ContinueOnError)
	fl.SetOutput(os.Stderr)
	browser := fl.Bool("browser", false, "serve on a loopback port behind a session token instead of opening a window")
	addr := fl.String("addr", "127.0.0.1:0", "browser-mode listen address")
	version := fl.Bool("version", false, "print the version and exit")
	var dev *bool
	if allowDev {
		dev = fl.Bool("dev", false, "development mode: developer tools and control commands on stdin")
	}
	if err := fl.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *version {
		info := BuildInfo
		if info == "" {
			if m, err := ReadManifestFS(appRoot(embedded)); err == nil {
				info = m.ProductName + " " + m.Version
			}
		}
		fmt.Fprintln(ctl, info)
		return 0
	}
	stopProfile := StartCPUProfile(os.Stderr)
	defer stopProfile()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := RunEmbedded(embedded, RunOptions{
		Browser: *browser,
		Addr:    *addr,
		Out:     ctl,
		Context: ctx,
		GoGlue:  register,
		Dev:     dev != nil && *dev,
		OnReady: func(c *Control) {
			fmt.Fprintln(ctl, "ready")
			if dev != nil && *dev {
				if !ServeControl(os.Stdin, ctl, c) {
					c.Quit()
				}
			}
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "krate:", err)
		return 1
	}
	return 0
}

// StartCPUProfile starts a CPU profile when ProfileEnv is set and returns the
// function that stops it and closes the file. A failure is reported on logw
// and leaves the process running without a profile.
func StartCPUProfile(logw io.Writer) (stop func()) {
	return gohost.StartCPUProfileAt(os.Getenv(ProfileEnv), logw)
}
