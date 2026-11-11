package updater

import (
	"context"
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

	err := u.fetchInto(ctx, part, progress)
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

func (u *Update) fetchInto(ctx context.Context, part string, progress func(done, total int64)) error {
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}
	if u.Size > 0 && have > u.Size {
		os.Remove(part)
		have = 0
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.URL, nil)
		if err != nil {
			return err
		}
		if have > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
		}
		res, err := u.cfg.client().Do(req)
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
			total = u.Size
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
