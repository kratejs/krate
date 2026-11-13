package updater

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Errors returned by the package.
var (
	// ErrNotConfigured: Config has no URL or public key.
	ErrNotConfigured = errors.New("updater: not configured (set native.updater.url and native.updater.publicKey)")
	// ErrBadSignature: the downloaded file does not match its signature.
	ErrBadSignature = errors.New("updater: signature does not match the downloaded file")
	// ErrNotDownloaded: Apply was called before Download finished.
	ErrNotDownloaded = errors.New("updater: the update has not been downloaded")
)

// Feed is the JSON document a feed URL returns.
type Feed struct {
	Version   string `json:"version"`
	Notes     string `json:"notes,omitempty"`
	PubDate   string `json:"pub_date,omitempty"`
	URL       string `json:"url"`
	Signature string `json:"signature"`
	Size      int64  `json:"size,omitempty"`

	// Patches are optional delta updates from older versions. A client takes
	// the entry whose From is its own version and falls back to URL when the
	// patch cannot be used.
	Patches []FeedPatch `json:"patches,omitempty"`
	// Rollout limits who is offered the update. Nil means everyone.
	Rollout *Rollout `json:"rollout,omitempty"`
	// Critical offers the update to every client whatever Rollout says.
	Critical bool `json:"critical,omitempty"`
	// MinVersion: a client running an older version is offered the update
	// whatever Rollout says, and Update.Required is set.
	MinVersion string `json:"min_version,omitempty"`
}

// FeedPatch is one delta update (see GeneratePatch).
type FeedPatch struct {
	// From is the version the patch applies to.
	From string `json:"from"`
	URL  string `json:"url"`
	// Signature is the SignPatch signature of the patch file.
	Signature string `json:"signature"`
	Size      int64  `json:"size,omitempty"`
	// SHA512 is the lowercase hex digest of the patch file.
	SHA512 string `json:"sha512,omitempty"`
}

// ParseFeed decodes and validates a feed document.
func ParseFeed(b []byte) (*Feed, error) {
	var f Feed
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("updater: feed is not valid JSON: %w", err)
	}
	if _, ok := parseVersion(f.Version); !ok {
		return nil, fmt.Errorf("updater: feed version %q is not a semantic version", f.Version)
	}
	if f.URL == "" {
		return nil, errors.New("updater: feed has no url")
	}
	sig, err := base64.StdEncoding.DecodeString(f.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("updater: feed signature is not a base64 Ed25519 signature")
	}
	if f.Size < 0 {
		return nil, errors.New("updater: feed size is negative")
	}
	if f.MinVersion != "" {
		if _, ok := parseVersion(f.MinVersion); !ok {
			return nil, fmt.Errorf("updater: feed min_version %q is not a semantic version", f.MinVersion)
		}
	}
	if f.Rollout != nil {
		if err := f.Rollout.validate(); err != nil {
			return nil, err
		}
	}
	return &f, nil
}

// ExpandURL fills {target} and {version} in a feed URL template.
func ExpandURL(tmpl, target, version string) string {
	return ExpandURLChannel(tmpl, target, version, "")
}

// ExpandURLChannel is ExpandURL with {channel} too (default "stable").
func ExpandURLChannel(tmpl, target, version, channel string) string {
	if channel == "" {
		channel = DefaultChannel
	}
	return strings.NewReplacer(
		"{target}", url.PathEscape(target),
		"{version}", url.PathEscape(version),
		"{channel}", url.PathEscape(channel),
	).Replace(tmpl)
}

// DecodePublicKey parses a base64 Ed25519 public key.
func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("updater: public key must be 32 bytes, base64 encoded")
	}
	return ed25519.PublicKey(b), nil
}

// GenerateKey returns a new key pair as base64 strings (private key is the
// 32-byte seed, public key the 32-byte point).
func GenerateKey() (private, public string, err error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(priv.Seed()), base64.StdEncoding.EncodeToString(pub), nil
}

// PrivateKeyFromSeed decodes a base64 seed produced by GenerateKey.
func PrivateKeyFromSeed(s string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.SeedSize {
		return nil, errors.New("updater: private key must be a 32-byte seed, base64 encoded")
	}
	return ed25519.NewKeyFromSeed(b), nil
}

// Config describes the app and where its updates come from.
type Config struct {
	// URL is the feed URL template; PublicKey verifies artifacts.
	URL       string
	PublicKey string
	// AppID is the running app's identifier (native.appId); it is part of the
	// signed message, so an artifact built for another app does not verify.
	AppID string
	// Version is the running version (semver).
	Version string
	// Target defaults to GOOS-GOARCH of the running process.
	Target string
	// Channel fills {channel} in URL; default "stable". A channel chosen at
	// run time with SetChannel (kept in StateDir) takes precedence.
	Channel string
	// StateDir holds the install id, the rollback state and the chosen
	// channel. Without it an update does not keep the previous version for
	// rollback and a staged rollout uses an id that lasts for the process.
	StateDir string
	// InstallID overrides the persisted random install id (tests).
	InstallID string
	// Now overrides the clock (tests).
	Now func() time.Time
	// Logf receives diagnostics, for example why a delta update fell back to
	// the full download. Default: discard.
	Logf func(format string, args ...any)
	// CacheDir holds staged downloads; required for Download.
	CacheDir string
	// Client defaults to a client with a 30 s timeout for the feed. Downloads
	// use the same client without a total timeout.
	Client *http.Client
	// Insecure allows http:// feed and artifact URLs (tests only).
	Insecure bool
	// Exe is the running executable; default os.Executable.
	Exe string
	// FS is the file system Apply works on; default the real one.
	FS FS
	// AppImage is the path of the running AppImage; default $APPIMAGE.
	AppImage string
	// GOOS decides the replace strategy of Apply; default runtime.GOOS.
	GOOS string
}

// Update is an available update.
type Update struct {
	Version string
	Notes   string
	PubDate string
	URL     string
	Size    int64
	// Critical is the feed's flag; Required is set when the running version
	// is older than the feed's min_version. Both bypass a staged rollout.
	Critical bool
	Required bool

	cfg       Config
	pub       ed25519.PublicKey
	signature string
	path      string // downloaded and verified file
	patch     *FeedPatch
	patchURL  string
	stage     string // new tree built from a patch, ready for Apply
	deltaErr  error
}

// Path returns the verified downloaded file, "" before Download finishes (and
// after a delta update, which never downloads the full file).
func (u *Update) Path() string { return u.path }

// Delta reports whether the downloaded update is a delta update.
func (u *Update) Delta() bool { return u.stage != "" }

// HasDelta reports whether the feed offered a patch for the running version.
func (u *Update) HasDelta() bool { return u.patch != nil }

// DeltaError is why the last Download fell back to the full artifact, nil
// when it did not.
func (u *Update) DeltaError() error { return u.deltaErr }

// DownloadSize is the number of bytes Download fetches: the patch size when a
// patch is offered, Size otherwise. 0 when unknown.
func (u *Update) DownloadSize() int64 {
	if u.patch != nil && u.patch.Size > 0 {
		return u.patch.Size
	}
	return u.Size
}

func (c *Config) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

func (c *Config) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Config) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{}
}

func (c *Config) target() string {
	if c.Target != "" {
		return c.Target
	}
	return goos(c) + "-" + goarch()
}

func (c *Config) checkScheme(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("updater: bad URL %q: %w", raw, err)
	}
	if u.Scheme == "https" || (c.Insecure && u.Scheme == "http") {
		return nil
	}
	return fmt.Errorf("updater: %q is not an https URL", raw)
}

// Check asks the feed for an update. It returns nil, nil when the app is up
// to date.
func Check(ctx context.Context, cfg Config) (*Update, error) {
	if cfg.URL == "" || cfg.PublicKey == "" {
		return nil, ErrNotConfigured
	}
	if cfg.AppID == "" {
		return nil, errors.New("updater: Config.AppID is required (the signature binds the artifact to the app id)")
	}
	pub, err := DecodePublicKey(cfg.PublicKey)
	if err != nil {
		return nil, err
	}
	cur, ok := parseVersion(cfg.Version)
	if !ok {
		return nil, fmt.Errorf("updater: running version %q is not a semantic version", cfg.Version)
	}
	feedURL := ExpandURLChannel(cfg.URL, cfg.target(), cfg.Version, cfg.channel())
	if err := cfg.checkScheme(feedURL); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := cfg.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("updater: fetching the feed: %w", err)
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusNoContent, http.StatusNotFound:
		return nil, nil
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("updater: feed answered HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("updater: reading the feed: %w", err)
	}
	f, err := ParseFeed(body)
	if err != nil {
		return nil, err
	}
	next, _ := parseVersion(f.Version)
	if compareVersions(next, cur) <= 0 {
		return nil, nil
	}
	artifact, err := res.Request.URL.Parse(f.URL)
	if err != nil {
		return nil, fmt.Errorf("updater: bad artifact URL %q: %w", f.URL, err)
	}
	if err := cfg.checkScheme(artifact.String()); err != nil {
		return nil, err
	}
	u := &Update{
		Version: f.Version, Notes: f.Notes, PubDate: f.PubDate, URL: artifact.String(), Size: f.Size,
		Critical: f.Critical,
		cfg:      cfg, pub: pub, signature: f.Signature,
	}
	if f.MinVersion != "" {
		if min, ok := parseVersion(f.MinVersion); ok && compareVersions(cur, min) < 0 {
			u.Required = true
		}
	}
	if cfg.StateDir != "" && cfg.failedVersion(f.Version) {
		cfg.logf("updater: version %s was rolled back on this install; not offering it again", f.Version)
		return nil, nil
	}
	if !u.Critical && !u.Required && f.Rollout != nil {
		id := cfg.installID()
		if !f.Rollout.Includes(id, f.Version, cfg.now()) {
			cfg.logf("updater: %s is in a staged rollout that does not include this install yet", f.Version)
			return nil, nil
		}
	}
	for i := range f.Patches {
		p := &f.Patches[i]
		from, ok := parseVersion(p.From)
		if !ok || compareVersions(from, cur) != 0 || p.URL == "" || p.Signature == "" {
			continue
		}
		pu, err := res.Request.URL.Parse(p.URL)
		if err != nil || cfg.checkScheme(pu.String()) != nil {
			continue
		}
		// A patch that is not smaller than the full file saves nothing.
		if p.Size > 0 && f.Size > 0 && p.Size >= f.Size {
			continue
		}
		u.patch, u.patchURL = p, pu.String()
		break
	}
	return u, nil
}

// Validate checks a feed built in code the way ParseFeed checks one read from
// the network.
func (f *Feed) Validate() error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	_, err = ParseFeed(b)
	return err
}
