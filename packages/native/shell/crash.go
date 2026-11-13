package shell

import (
	"os"
	"runtime/debug"

	"github.com/kratejs/krate/packages/native/crashreport"
	"github.com/kratejs/krate/packages/native/gohost"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
)

// setupCrash points the crash report store at <logDir>/crashes, hands the
// directory to child processes through KRATE_CRASH_DIR and installs the Go
// crash hook, so a panic or fatal error that ends the shell leaves a log the
// next launch turns into a report. A failure is not an error: the app runs
// without crash reports.
func (a *app) setupCrash(man *Manifest) {
	logDir, err := sdkos.LogDir()
	if err != nil {
		a.logf("crash reports are off: %v", err)
		return
	}
	dir := logDir + string(os.PathSeparator) + "crashes"
	osVersion := sdkos.GetInfo().OSVersion
	shellVersion := ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		shellVersion = bi.Main.Version
	}
	crashreport.Configure(crashreport.Config{
		Dir: dir, AppID: man.AppID, AppVersion: man.Version, OSVersion: osVersion,
		ShellVersion: shellVersion, Process: "shell",
	})
	os.Setenv(crashreport.DirEnv, dir)
	a.crashStop = crashreport.Install()
}

// watchChildCrash collects the crash log of the Go host child when it exits,
// so a panic of the child becomes a report and a crash event.
func watchChildCrash(c *gohost.Child) {
	ex := c.Exited()
	if ex == nil {
		return
	}
	go func() {
		<-ex
		crashreport.Collect()
	}()
}
