package gohost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kratejs/krate/packages/native/scheme"
)

// Serve is the whole main of a generated Go host child:
//
//	func main() { gohost.Serve(glue.Register) }
//
// It never returns normally: it exits the process when the parent goes away
// (stdin EOF), on SIGTERM or interrupt, and with status 1 on a startup error.
func Serve(register func(*scheme.Registry)) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := Run(ctx, register, Config{Socket: os.Getenv(SocketEnv), Stdin: os.Stdin, Ready: os.Stdout})
	if err != nil {
		fmt.Fprintln(os.Stderr, "krate gohost:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Config is Serve's environment, separated for tests.
type Config struct {
	// Socket is the unix socket path to listen on.
	Socket string
	// Stdin is watched for EOF; nil disables the check.
	Stdin io.Reader
	// Ready receives "ready\n" once the child is listening.
	Ready io.Writer
}

// Run serves until ctx ends or cfg.Stdin reaches EOF. It returns nil on a
// clean stop.
func Run(ctx context.Context, register func(*scheme.Registry), cfg Config) error {
	if cfg.Socket == "" {
		return fmt.Errorf("%s is not set; this binary is started by the Krate shell", SocketEnv)
	}
	reg := scheme.NewRegistry()
	if register != nil {
		register(reg)
	}
	// A stale file from a crashed run would make Listen fail.
	_ = os.Remove(cfg.Socket)
	ln, err := net.Listen("unix", cfg.Socket)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.Socket, err)
	}
	defer os.Remove(cfg.Socket)

	srv := &http.Server{Handler: scheme.New(scheme.Options{Registry: reg, Dev: true})}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	if cfg.Ready != nil {
		if _, err := io.WriteString(cfg.Ready, ReadyLine+"\n"); err != nil {
			srv.Close()
			return err
		}
	}

	parentGone := make(chan struct{})
	if cfg.Stdin != nil {
		go func() {
			_, _ = io.Copy(io.Discard, cfg.Stdin)
			close(parentGone)
		}()
	}

	select {
	case <-ctx.Done():
	case <-parentGone:
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	// Streams and signals never end on their own, so do not wait for them.
	cctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = srv.Shutdown(cctx)
	_ = srv.Close()
	return nil
}
