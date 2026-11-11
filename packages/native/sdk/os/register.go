package sdkos

import (
	"os/exec"
	"runtime"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/glue"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

func init() {
	krate.RegisterCapability("os.info", func() bool { return true })
	krate.RegisterCapability("os.openExternal", func() bool {
		switch runtime.GOOS {
		case "darwin", "windows":
			return true
		}
		_, err := exec.LookPath("xdg-open")
		return err == nil
	})
	krate.RegisterCapability("os.secrets", secretsSupported)
}

// Register exposes the os area to the UI as krate/os.* ids, gated by set.
func Register(r *scheme.Registry, set permission.Set) {
	dir := func(id string, f func() (string, error)) {
		permission.Register(r, set, id, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
			a, err := glue.Open(id, args)
			if err != nil {
				return err
			}
			if err := a.Done(); err != nil {
				return err
			}
			p, err := f()
			if err != nil {
				return glue.Code(err)
			}
			out.String(p)
			return nil
		})
	}
	dir("krate/os.appDataDir", AppDataDir)
	dir("krate/os.cacheDir", CacheDir)
	dir("krate/os.configDir", ConfigDir)
	dir("krate/os.logDir", LogDir)
	dir("krate/os.tempDir", TempDir)

	permission.Register(r, set, "krate/os.info", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		a, err := glue.Open("krate/os.info", args)
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		encodeInfo(out, GetInfo())
		return nil
	})

	permission.Register(r, set, "krate/os.openExternal", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		const id = "krate/os.openExternal"
		a, err := glue.Open(id, args)
		if err != nil {
			return err
		}
		u, err := a.String("url")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		if err := OpenExternal(ctx, u); err != nil {
			return glue.Code(err)
		}
		out.Null()
		return nil
	})

	permission.Register(r, set, "krate/os.secretSet", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		const id = "krate/os.secretSet"
		a, err := glue.Open(id, args)
		if err != nil {
			return err
		}
		key, err := a.String("key")
		if err != nil {
			return err
		}
		val, err := a.String("value")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		if err := SecretSet(ctx, key, val); err != nil {
			return glue.Code(err)
		}
		out.Null()
		return nil
	})

	permission.Register(r, set, "krate/os.secretGet", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		const id = "krate/os.secretGet"
		a, err := glue.Open(id, args)
		if err != nil {
			return err
		}
		key, err := a.String("key")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		v, err := SecretGet(ctx, key)
		if err == ErrSecretNotFound {
			out.Null()
			return nil
		}
		if err != nil {
			return glue.Code(err)
		}
		out.String(v)
		return nil
	})

	permission.Register(r, set, "krate/os.secretDelete", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		const id = "krate/os.secretDelete"
		a, err := glue.Open(id, args)
		if err != nil {
			return err
		}
		key, err := a.String("key")
		if err != nil {
			return err
		}
		if err := a.Done(); err != nil {
			return err
		}
		if err := SecretDelete(ctx, key); err != nil {
			return glue.Code(err)
		}
		out.Null()
		return nil
	})
}

func encodeInfo(e *wire.Encoder, i Info) {
	e.BeginObject()
	for _, kv := range [...][2]string{
		{"os", i.OS}, {"arch", i.Arch}, {"osVersion", i.OSVersion}, {"hostname", i.Hostname},
		{"locale", i.Locale}, {"appId", i.AppID}, {"appVersion", i.AppVersion},
	} {
		e.Key(kv[0])
		e.String(kv[1])
	}
	e.EndObject()
}
