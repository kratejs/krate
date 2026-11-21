// Package matc finds, downloads and runs Filament's matc material compiler
// for custom (shaderMaterial) shaders. The matc binary is never embedded in
// krate: it is fetched on first use into ~/.krate/tools/filament/<version>.
package matc

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// FilamentVersion is the Filament release the pre-compiled materials in
// packages/3d/materials were built with. It must match materials/VERSION.
const FilamentVersion = "1.53.4"

// DefaultBaseURL hosts the Filament release archives.
const DefaultBaseURL = "https://github.com/google/filament/releases/download"

// Environment variables read by Resolve.
const (
	EnvMatc    = "KRATE_FILAMENT_MATC"
	EnvOffline = "KRATE_OFFLINE"
)

// matcFlags are the matc arguments for WebGL 2 output. They mirror
// packages/3d/materials/build.mjs.
var matcFlags = []string{"-a", "opengl", "-p", "mobile", "--optimize-size", "--variant-filter=stereo,vsm,fog,ssr"}

// Options makes the environment injectable. The zero value uses the process
// environment, the user's home directory and the real Filament release host.
type Options struct {
	// HomeDir holds the .krate directory. Defaults to os.UserHomeDir.
	HomeDir string
	// BaseURL is the release download root. Defaults to DefaultBaseURL.
	BaseURL string
	// Version defaults to FilamentVersion.
	Version string
	// GOOS selects the archive and binary name. Defaults to runtime.GOOS.
	GOOS string
	// Getenv defaults to os.Getenv.
	Getenv func(string) string
	// Client defaults to a client with a generous timeout.
	Client *http.Client
}

// ShaderHash returns the 16 hex digit FNV-1a 64 hash of a material source.
// It names the compiled <hash>.filamat file.
func ShaderHash(src []byte) string {
	h := fnv.New64a()
	_, _ = h.Write(src)
	return fmt.Sprintf("%016x", h.Sum64())
}

// Resolve returns the path of a usable matc binary using default options.
func Resolve(ctx context.Context) (string, error) { return Options{}.Resolve(ctx) }

// Compile compiles a .mat source into outDir using default options.
func Compile(ctx context.Context, matSource []byte, outDir string) (string, bool, error) {
	return Options{}.Compile(ctx, matSource, outDir)
}

func (o Options) getenv(k string) string {
	if o.Getenv != nil {
		return o.Getenv(k)
	}
	return os.Getenv(k)
}

func (o Options) version() string {
	if o.Version != "" {
		return o.Version
	}
	return FilamentVersion
}

func (o Options) goos() string {
	if o.GOOS != "" {
		return o.GOOS
	}
	return runtime.GOOS
}

func (o Options) binName() string {
	if o.goos() == "windows" {
		return "matc.exe"
	}
	return "matc"
}

// platformAsset is the platform part of the release archive name.
func (o Options) platformAsset() (string, error) {
	switch o.goos() {
	case "linux":
		return "linux", nil
	case "darwin":
		return "mac", nil
	case "windows":
		return "windows", nil
	}
	return "", diag.New("", 0, 0,
		"no Filament release is published for "+o.goos(),
		"set "+EnvMatc+" to a matc binary built for this platform", "")
}

func (o Options) installDir() (string, error) {
	home := o.HomeDir
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", diag.New("", 0, 0, "cannot find the home directory for the matc cache: "+err.Error(),
				"set "+EnvMatc+" to a matc binary", "")
		}
		home = h
	}
	return filepath.Join(home, ".krate", "tools", "filament", o.version(), "bin"), nil
}

// Resolve returns the path of a matc binary. KRATE_FILAMENT_MATC wins, then
// the cached ~/.krate/tools/filament/<version>/bin/matc, then a download
// unless KRATE_OFFLINE=1.
func (o Options) Resolve(ctx context.Context) (string, error) {
	if p := o.getenv(EnvMatc); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", diag.New("", 0, 0, EnvMatc+" points to "+p+", which does not exist",
				"set "+EnvMatc+" to an existing matc binary or unset it to use the downloaded one", "")
		}
		return p, nil
	}
	dir, err := o.installDir()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, o.binName())
	if fi, err := os.Stat(bin); err == nil && !fi.IsDir() {
		return bin, nil
	}
	if o.getenv(EnvOffline) == "1" {
		return "", diag.New("", 0, 0,
			"custom <shaderMaterial> needs Filament's matc ("+o.version()+") but it is not installed and "+EnvOffline+"=1",
			"unset "+EnvOffline+" to let krate download it, or set "+EnvMatc+" to a matc binary", "")
	}
	if err := o.download(ctx, dir, bin); err != nil {
		return "", err
	}
	return bin, nil
}

func (o Options) download(ctx context.Context, dir, bin string) error {
	plat, err := o.platformAsset()
	if err != nil {
		return err
	}
	base := o.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	v := o.version()
	url := fmt.Sprintf("%s/v%s/filament-v%s-%s.tgz", base, v, v, plat)
	fail := func(msg string) error {
		return diag.New("", 0, 0, msg,
			"set "+EnvMatc+" to a matc binary, or download "+url+" and extract bin/"+o.binName()+" to "+bin, "")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fail("cannot download Filament matc: " + err.Error())
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fail("cannot download Filament matc: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail("cannot download Filament matc: " + url + " returned " + resp.Status)
	}
	if err := extractMatc(resp.Body, dir, bin, o.binName()); err != nil {
		return fail("cannot extract matc from the Filament archive: " + err.Error())
	}
	return nil
}

// extractMatc streams a .tgz and writes only the bin/<name> entry to bin via a
// temp file and an atomic rename, so parallel builds never see a partial file.
func extractMatc(r io.Reader, dir, bin, name string) error {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("archive has no bin/%s", name)
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != name || path.Base(path.Dir(hdr.Name)) != "bin" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(dir, "."+name+".tmp*")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		if _, err := io.Copy(tmp, tr); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return err
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpName)
			return err
		}
		if err := os.Chmod(tmpName, 0o755); err != nil {
			_ = os.Remove(tmpName)
			return err
		}
		if err := os.Rename(tmpName, bin); err != nil {
			_ = os.Remove(tmpName)
			// Another build may have won the race (Windows cannot rename over a running file).
			if fi, serr := os.Stat(bin); serr == nil && !fi.IsDir() {
				return nil
			}
			return err
		}
		return nil
	}
}

// Compile writes the .mat source to a temp file, runs matc on it and returns
// <outDir>/<ShaderHash>.filamat. cached reports that the file already existed.
func (o Options) Compile(ctx context.Context, matSource []byte, outDir string) (filamatPath string, cached bool, err error) {
	out := filepath.Join(outDir, ShaderHash(matSource)+".filamat")
	if fi, serr := os.Stat(out); serr == nil && !fi.IsDir() {
		return out, true, nil
	}
	matc, err := o.Resolve(ctx)
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", false, err
	}
	tmpDir, err := os.MkdirTemp(outDir, ".matc-*")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(tmpDir)

	in := filepath.Join(tmpDir, "material.mat")
	if err := os.WriteFile(in, matSource, 0o644); err != nil {
		return "", false, err
	}
	tmpOut := filepath.Join(tmpDir, "material.filamat")
	args := append(append([]string{}, matcFlags...), "-o", tmpOut, in)
	_, stderr, runErr := execx.Run(ctx, 2*time.Minute, tmpDir, nil, matc, args...)
	if runErr != nil {
		msg := "matc failed to compile the shaderMaterial: " + runErr.Error()
		if len(stderr) > 0 {
			msg += "\n" + string(stderr)
		}
		return "", false, diag.New("", 0, 0, msg,
			"fix the GLSL reported by matc; the material source hash is "+ShaderHash(matSource), "")
	}
	data, err := os.ReadFile(tmpOut)
	if err != nil {
		return "", false, diag.New("", 0, 0, "matc produced no output file: "+err.Error(),
			"check that "+matc+" is Filament "+o.version()+" matc (run it with --version)", "")
	}
	if err := fsutil.WriteFileAtomic(out, data, 0o644); err != nil {
		return "", false, err
	}
	return out, false, nil
}
