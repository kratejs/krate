package updater

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/sha512"
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
	return &f, nil
}

// ExpandURL fills {target} and {version} in a feed URL template.
func ExpandURL(tmpl, target, version string) string {
	return strings.NewReplacer(
		"{target}", url.PathEscape(target),
		"{version}", url.PathEscape(version),
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

var prehash = &ed25519.Options{Hash: crypto.SHA512}

// Sign returns the base64 Ed25519ph signature of the data read from r.
func Sign(priv ed25519.PrivateKey, r io.Reader) (string, error) {
	h := sha512.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	sig, err := priv.Sign(nil, h.Sum(nil), prehash)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// Verify checks a base64 signature of the data read from r.
func Verify(pub ed25519.PublicKey, r io.Reader, signature string) error {
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	h := sha512.New()
	if _, err := io.Copy(h, r); err != nil {
		return err
	}
	if ed25519.VerifyWithOptions(pub, h.Sum(nil), sig, prehash) != nil {
		return ErrBadSignature
	}
	return nil
}

// Config describes the app and where its updates come from.
type Config struct {
	// URL is the feed URL template; PublicKey verifies artifacts.
	URL       string
	PublicKey string
	// Version is the running version (semver).
	Version string
	// Target defaults to GOOS-GOARCH of the running process.
	Target string
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

	cfg       Config
	pub       ed25519.PublicKey
	signature string
	path      string // downloaded and verified file
}

// Path returns the verified downloaded file, "" before Download finishes.
func (u *Update) Path() string { return u.path }

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
	pub, err := DecodePublicKey(cfg.PublicKey)
	if err != nil {
		return nil, err
	}
	cur, ok := parseVersion(cfg.Version)
	if !ok {
		return nil, fmt.Errorf("updater: running version %q is not a semantic version", cfg.Version)
	}
	feedURL := ExpandURL(cfg.URL, cfg.target(), cfg.Version)
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
	return &Update{
		Version: f.Version, Notes: f.Notes, PubDate: f.PubDate, URL: artifact.String(), Size: f.Size,
		cfg: cfg, pub: pub, signature: f.Signature,
	}, nil
}
