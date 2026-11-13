package updater

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// stagedName is the file name an artifact is stored under.
func stagedName(rawURL string) string {
	base := path.Base(strings.SplitN(rawURL, "?", 2)[0])
	base = unsafeName.ReplaceAllString(base, "_")
	base = strings.Trim(base, ".")
	if base == "" {
		base = "update"
	}
	return base
}

// Download fetches the artifact into <CacheDir>/updates/<version>, verifies
// its signature and records the path for Apply. An interrupted download
// resumes from the partial file when the server supports Range requests.
// progress, when set, is called with the bytes done and the total (0 when
// unknown). Calling Download again after success is a cheap re-verification.
func (u *Update) Download(ctx context.Context, progress func(done, total int64)) error {
	if u.cfg.CacheDir == "" {
		return errors.New("updater: no cache directory configured")
	}
	dir := filepath.Join(u.cfg.CacheDir, "updates", unsafeName.ReplaceAllString(u.Version, "_"))
	if cur, ok := parseVersion(u.cfg.Version); ok {
		if next, ok := parseVersion(u.Version); !ok || compareVersions(next, cur) <= 0 {
			return fmt.Errorf("updater: refusing version %q: not newer than the running %q", u.Version, u.cfg.Version)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	final := filepath.Join(dir, stagedName(u.URL))
	part := final + ".part"
	if progress == nil {
		progress = func(int64, int64) {}
	}

	// A finished, verified file from an earlier run needs no network.
	if st, err := os.Stat(final); err == nil && st.Mode().IsRegular() {
		if u.verifyFile(final) == nil {
			progress(st.Size(), st.Size())
			u.path = final
			return nil
		}
		os.Remove(final)
	}
	if u.stage != "" {
		if st, err := os.Stat(u.stage); err == nil && st.IsDir() {
			return nil
		}
		u.stage = ""
	}

	if u.patch != nil {
		err := u.downloadDelta(ctx, dir, progress)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		u.deltaErr = err
		u.cfg.logf("updater: delta update failed, downloading the full file: %v", err)
	}

	err := u.cfg.fetchInto(ctx, u.URL, part, u.Size, progress)
	if err != nil {
		return err
	}
	if err := u.verifyFile(part); err != nil {
		os.Remove(part)
		return err
	}
	if err := os.Rename(part, final); err != nil {
		return err
	}
	u.path = final
	return nil
}

// downloadDelta fetches and verifies the patch and builds the new tree from
// the installed files into a staging directory. Any error means the caller
// should download the full file.
func (u *Update) downloadDelta(ctx context.Context, dir string, progress func(done, total int64)) error {
	p := u.patch
	final := filepath.Join(dir, stagedName(u.patchURL))
	part := final + ".part"
	verify := func(name string) error {
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		if p.Size > 0 {
			if st, err := f.Stat(); err == nil && st.Size() != p.Size {
				return fmt.Errorf("updater: patch is %d bytes, the feed says %d", st.Size(), p.Size)
			}
		}
		if p.SHA512 != "" {
			sum, _, err := digest(f)
			if err != nil {
				return err
			}
			if !strings.EqualFold(hex.EncodeToString(sum), p.SHA512) {
				return errors.New("updater: patch does not match the digest in the feed")
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return err
			}
		}
		return VerifyPatch(u.pub, PatchMeta{AppID: u.cfg.AppID, Target: u.cfg.target(), From: p.From, To: u.Version}, f, p.Signature)
	}
	if st, err := os.Stat(final); err == nil && st.Mode().IsRegular() && verify(final) == nil {
		progress(st.Size(), st.Size())
	} else {
		os.Remove(final)
		if err := u.cfg.fetchInto(ctx, u.patchURL, part, p.Size, progress); err != nil {
			return err
		}
		if err := verify(part); err != nil {
			os.Remove(part)
			return err
		}
		if err := os.Rename(part, final); err != nil {
			return err
		}
	}
	return u.stageDelta(final)
}

// stageDelta applies the verified patch file to the installed files.
func (u *Update) stageDelta(patchFile string) error {
	exe, err := u.cfg.executable()
	if err != nil {
		return err
	}
	fsys := u.cfg.fs()
	var kind, base, dst string
	switch {
	case goos(&u.cfg) == "darwin":
		base = bundleOf(exe)
		if base == "" {
			return fmt.Errorf("updater: %s is not inside a .app bundle", exe)
		}
		kind, dst = KindBundle, filepath.Base(base)
	case goos(&u.cfg) == "linux" && u.cfg.appImage() != "":
		base = u.cfg.appImage()
		kind, dst = KindFile, filepath.Base(base)
	default:
		kind, base = KindTree, filepath.Dir(exe)
	}
	stage := stagePath(kind, base, u.Version)
	_ = fsys.RemoveAll(stage)
	if err := buildPatched(fsys, patchFile, kind, base, stage, dst, u.patch.From, u.Version); err != nil {
		_ = fsys.RemoveAll(stage)
		return err
	}
	u.stage = stage
	return nil
}

func (u *Update) verifyFile(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	if u.Size > 0 {
		if st, err := f.Stat(); err == nil && st.Size() != u.Size {
			return fmt.Errorf("updater: downloaded file is %d bytes, the feed says %d", st.Size(), u.Size)
		}
	}
	return Verify(u.pub, u.meta(), f, u.signature)
}

func (c *Config) fetchInto(ctx context.Context, rawURL, part string, size int64, progress func(done, total int64)) error {
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}
	if size > 0 && have > size {
		os.Remove(part)
		have = 0
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return err
		}
		if have > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
		}
		res, err := c.client().Do(req)
		if err != nil {
			return fmt.Errorf("updater: downloading: %w", err)
		}
		flags := os.O_WRONLY | os.O_CREATE
		var total int64
		switch res.StatusCode {
		case http.StatusPartialContent:
			if start, size, ok := parseContentRange(res.Header.Get("Content-Range")); !ok || start != have {
				res.Body.Close()
				return errors.New("updater: server sent an unexpected byte range")
			} else {
				total = size
			}
			flags |= os.O_APPEND
		case http.StatusOK:
			// The server ignored Range (or none was sent): start over.
			have = 0
			flags |= os.O_TRUNC
			total = res.ContentLength
		case http.StatusRequestedRangeNotSatisfiable:
			res.Body.Close()
			if attempt == 0 && have > 0 {
				// The partial file does not fit the artifact: start over.
				os.Remove(part)
				have = 0
				continue
			}
			return errors.New("updater: server rejected the byte range")
		default:
			res.Body.Close()
			return fmt.Errorf("updater: download answered HTTP %d", res.StatusCode)
		}
		if total <= 0 {
			total = size
		}
		f, err := os.OpenFile(part, flags, 0o600)
		if err != nil {
			res.Body.Close()
			return err
		}
		done := have
		progress(done, total)
		buf := make([]byte, 64<<10)
		var rerr error
		for {
			n, err := res.Body.Read(buf)
			if n > 0 {
				if _, werr := f.Write(buf[:n]); werr != nil {
					rerr = werr
					break
				}
				done += int64(n)
				progress(done, total)
			}
			if err != nil {
				if err != io.EOF {
					rerr = err
				}
				break
			}
		}
		res.Body.Close()
		if cerr := f.Close(); rerr == nil {
			rerr = cerr
		}
		if rerr != nil {
			return fmt.Errorf("updater: downloading: %w", rerr)
		}
		if total > 0 && done != total {
			return fmt.Errorf("updater: download ended after %d of %d bytes", done, total)
		}
		return nil
	}
	return errors.New("updater: download failed")
}

// parseContentRange reads "bytes START-END/TOTAL".
func parseContentRange(h string) (start, total int64, ok bool) {
	var end int64
	if n, _ := fmt.Sscanf(h, "bytes %d-%d/%d", &start, &end, &total); n != 3 {
		return 0, 0, false
	}
	return start, total, true
}
