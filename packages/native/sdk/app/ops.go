package app

import (
	"os"
	"os/exec"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/internal/picked"
	"github.com/kratejs/krate/packages/native/wire"
)

// Register adds the platform-side operations to t. support answers
// Supports; relaunch, when non-nil, replaces the default (start os.Args again
// and quit).
func Register(t *bridge.Table, env *bridge.Env, support func(capability string) bool) {
	topic := env.Topic("app")
	t.Stream(IDEvents, Permission, true, topic.Stream())

	t.Func(IDQuit, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDQuit).Done(); err != nil {
			return err
		}
		out.Null()
		env.App.Quit()
		return nil
	})
	t.Func(IDRelaunch, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDRelaunch).Done(); err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }() // reap the child
		out.Null()
		env.App.Quit()
		return nil
	})
	t.Func(IDSupports, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSupports)
		c := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		out.Bool(support != nil && support(c))
		return nil
	})
	t.Func(IDLaunchArgs, "", false, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDLaunchArgs).Done(); err != nil {
			return err
		}
		codec.WriteStrings(out, launchArgs())
		return nil
	})

	raise := func() {
		if c, _, _, err := env.Controls(""); err == nil {
			c.Show()
			c.Focus()
		}
	}
	files := registerOpenFiles(t, env, env.Topic("appopen"), picked.Notify, raise)

	// macOS hands URLs of the app's schemes to the running app instead of
	// starting it with a command line; they reach OnOpenURL like the ones a
	// second launch carries.
	if ou, ok := env.App.(caps.OpenURLs); ok {
		ou.OnOpenURLs(func(urls []string) {
			topic.Publish(func(e *wire.Encoder) { encodeSecond(e, SecondInstance{Args: urls, URLs: urls}) })
			raise()
		})
	}

	var mu sync.Mutex
	var held *Instance
	t.Func(IDSingleInstance, "", false, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSingleInstance)
		id := r.String()
		argv := r.Strings()
		if err := r.Done(); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		if held != nil {
			out.Bool(true)
			return nil
		}
		inst, first, err := Acquire("", id, argv, func(si SecondInstance) {
			files.add(sourceSecond, FilesFromArgs(si.Args, si.Dir))
			topic.Publish(func(e *wire.Encoder) { encodeSecond(e, si) })
			raise()
		})
		if err != nil {
			return err
		}
		held = inst
		out.Bool(first)
		return nil
	})
}
