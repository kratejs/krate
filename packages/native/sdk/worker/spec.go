package worker

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Restart policies.
const (
	// RestartNever leaves an exited worker stopped.
	RestartNever = "never"
	// RestartOnFailure restarts a worker that crashed or exited non-zero.
	RestartOnFailure = "onFailure"
	// RestartAlways also restarts a worker that exited cleanly.
	RestartAlways = "always"
)

// Restart is a worker's restart policy.
type Restart struct {
	// Policy is RestartNever (default), RestartOnFailure or RestartAlways.
	Policy string `json:"policy,omitempty"`
	// Max is how many restarts in a row are allowed before the worker is
	// marked failed. Zero means 5. A run longer than StableAfter resets the
	// count.
	Max int `json:"max,omitempty"`
	// BackoffMs is the delay before the first restart (default 200); it
	// doubles for each restart in a row up to 10 s.
	BackoffMs int `json:"backoffMs,omitempty"`
}

// Spec declares one worker. The build writes specs into app.json under
// "workers"; pages and host code start them by name.
type Spec struct {
	Name string `json:"name"`
	// Runtime is "node", "bun", "deno" (a bundled TypeScript module), "go" or
	// "exec" (an executable that serves the host child protocol).
	Runtime string `json:"runtime"`
	// Entry is the worker's file, relative to the app directory (see the host
	// children in shell.HostChildSpec).
	Entry string `json:"entry"`
	// Command is the argv that starts a script entry; "{entry}" stands for the
	// absolute path of Entry. Empty means Entry is the executable.
	Command []string `json:"command,omitempty"`
	// Args are extra arguments appended to Command (or passed to Entry).
	Args []string `json:"args,omitempty"`
	// Permissions narrows what the worker may call in the SDK: a subset of the
	// app's permissions. Empty or missing means nothing. The worker's own
	// messaging (worker.emit) is always allowed.
	Permissions []string `json:"permissions,omitempty"`
	Restart     Restart  `json:"restart,omitempty"`
	// Autostart starts one instance when the app starts.
	Autostart bool `json:"autostart,omitempty"`
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// Validate checks a spec.
func (s Spec) Validate() error {
	if !nameRE.MatchString(s.Name) {
		return fmt.Errorf("worker name %q: use letters, digits, '_', '.' and '-' (up to 64, not starting with a symbol)", s.Name)
	}
	if s.Entry == "" {
		return fmt.Errorf("worker %q: entry is empty", s.Name)
	}
	switch s.Runtime {
	case "node", "bun", "deno", "go", "exec":
	default:
		return fmt.Errorf("worker %q: runtime %q is not node, bun, deno, go or exec", s.Name, s.Runtime)
	}
	switch s.Restart.Policy {
	case "", RestartNever, RestartOnFailure, RestartAlways:
	default:
		return fmt.Errorf("worker %q: restart policy %q is not never, onFailure or always", s.Name, s.Restart.Policy)
	}
	if s.Restart.Max < 0 || s.Restart.BackoffMs < 0 {
		return fmt.Errorf("worker %q: restart max and backoffMs cannot be negative", s.Name)
	}
	return nil
}

// Limits of the restart loop.
const (
	defaultMaxRestarts = 5
	defaultBackoff     = 200 * time.Millisecond
	maxBackoff         = 10 * time.Second
	// StableAfter is how long a run must last to reset the restart count.
	StableAfter = 30 * time.Second
)

// Exit describes how a process ended.
type Exit struct {
	Code int // -1 when killed by a signal
	Err  error
	// Requested is true when Stop asked for the exit.
	Requested bool
	// Ran is how long the process lived.
	Ran time.Duration
}

// Failed reports whether the exit was a failure (not requested, non-zero).
func (e Exit) Failed() bool { return !e.Requested && (e.Code != 0 || e.Err != nil) }

// Decision is what the supervisor does after an exit.
type Decision struct {
	Restart bool
	Delay   time.Duration
	// GaveUp is true when the policy wanted a restart but the count ran out.
	GaveUp bool
	// Streak is the number of restarts in a row including this one.
	Streak int
}

// Decide applies the restart policy. streak is the number of restarts in a
// row before this exit.
func (r Restart) Decide(e Exit, streak int) Decision {
	if e.Requested {
		return Decision{}
	}
	want := false
	switch r.Policy {
	case RestartAlways:
		want = true
	case RestartOnFailure:
		want = e.Failed()
	}
	if !want {
		return Decision{}
	}
	if e.Ran >= StableAfter {
		streak = 0
	}
	max := r.Max
	if max == 0 {
		max = defaultMaxRestarts
	}
	if streak >= max {
		return Decision{GaveUp: true, Streak: streak}
	}
	d := defaultBackoff
	if r.BackoffMs > 0 {
		d = time.Duration(r.BackoffMs) * time.Millisecond
	}
	for i := 0; i < streak && d < maxBackoff; i++ {
		d *= 2
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	return Decision{Restart: true, Delay: d, Streak: streak + 1}
}

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// envDenied lists variables a start request may not set (compared in upper
// case: Windows names are case-insensitive). They change how a runtime or the
// dynamic loader loads code, which TLS certificates a worker trusts, or what
// the shell itself passes to the worker.
var envDenied = map[string]bool{
	"PATH": true, "PATHEXT": true, "COMSPEC": true, "IFS": true, "BASH_ENV": true, "ENV": true,
	"GODEBUG": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
	"PYTHONPATH": true, "PYTHONSTARTUP": true, "PYTHONHOME": true, "PERL5LIB": true, "PERL5OPT": true,
	"RUBYOPT": true, "RUBYLIB": true, "JAVA_TOOL_OPTIONS": true, "_JAVA_OPTIONS": true, "JDK_JAVA_OPTIONS": true,
	"ELECTRON_RUN_AS_NODE": true,
}

// envDeniedPrefixes are the families envDenied cannot list one by one. NODE_ENV
// is the one NODE_ variable allowed.
var envDeniedPrefixes = []string{"KRATE_", "LD_", "DYLD_", "NODE_", "BUN_", "DENO_"}

// ValidateEnv checks the environment a start request adds to a worker. A
// name that is not a plain identifier, a value with a NUL byte, or a variable
// of envDenied is refused: the page that may start a worker must not be able
// to run code of its choice in it (NODE_OPTIONS=--import=data:..., LD_PRELOAD)
// or point it at another SDK endpoint (KRATE_GOHOST_PARENT).
func ValidateEnv(env map[string]string) error {
	for k, v := range env {
		if !envNameRE.MatchString(k) {
			return fmt.Errorf("environment variable name %q is not a plain identifier", k)
		}
		if strings.ContainsRune(v, 0) {
			return fmt.Errorf("environment variable %s: the value contains a NUL byte", k)
		}
		u := strings.ToUpper(k)
		denied := envDenied[u]
		for _, p := range envDeniedPrefixes {
			if strings.HasPrefix(u, p) && u != "NODE_ENV" {
				denied = true
			}
		}
		if denied {
			return fmt.Errorf("environment variable %s cannot be set when a worker starts: it changes how the worker loads code or what the shell passes to it (put runtime flags in the worker's command)", k)
		}
	}
	return nil
}
