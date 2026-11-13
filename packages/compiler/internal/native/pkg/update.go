package pkg

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/native/updater"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Environment variables that hold the updater private key.
const (
	EnvUpdaterKey     = "KRATE_UPDATER_PRIVATE_KEY"
	EnvUpdaterKeyFile = "KRATE_UPDATER_PRIVATE_KEY_FILE"
)

// LoadUpdaterKey reads the updater private key from the environment: the
// base64 seed itself or the path of a file that holds it. It returns nil
// when neither is set.
func LoadUpdaterKey(getenv func(string) string) (ed25519.PrivateKey, error) {
	text := strings.TrimSpace(getenv(EnvUpdaterKey))
	if text == "" {
		if p := getenv(EnvUpdaterKeyFile); p != "" {
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, fail(diag.CodeNativeUpdaterKey, EnvUpdaterKeyFile, "cannot read the updater key file: "+err.Error(), "check the path in "+EnvUpdaterKeyFile)
			}
			text = strings.TrimSpace(string(b))
		}
	}
	if text == "" {
		return nil, nil
	}
	k, err := updater.PrivateKeyFromSeed(text)
	if err != nil {
		return nil, fail(diag.CodeNativeUpdaterKey, EnvUpdaterKey, "the updater private key is not valid: "+err.Error(), "create a key with krate native keygen")
	}
	return k, nil
}

// GenerateUpdaterKey creates an updater key pair, writes the private key
// (base64 seed) to path with mode 0600 and returns the public key. It refuses
// to overwrite a file unless force is set.
func GenerateUpdaterKey(path string, force bool) (string, error) {
	if _, err := os.Stat(path); err == nil && !force {
		return "", fail(diag.CodeNativeUpdaterKey, path, "the key file already exists", "pass --force to replace it (updates signed with the old key stop verifying), or choose another --key-out path")
	}
	priv, pub, err := updater.GenerateKey()
	if err != nil {
		return "", fail(diag.CodeNativeUpdaterKey, path, err.Error(), "")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", wrapWrite(path, err)
	}
	if err := os.WriteFile(path, []byte(priv+"\n"), 0o600); err != nil {
		return "", wrapWrite(path, err)
	}
	return pub, nil
}

// signUpdates signs every update artifact and writes latest-<target>.json.
func signUpdates(o *Options, res *Result) error {
	if o.UpdaterKey == nil {
		if o.Meta.Updater != nil {
			res.note("native.updater is configured but %s is not set: update files are not signed and no latest-%s-%s.json was written", EnvUpdaterKey, o.Payload.OS, o.Payload.Arch)
		}
		return nil
	}
	if o.Meta.Updater != nil && o.Meta.Updater.PublicKey != "" {
		pub, err := updater.DecodePublicKey(o.Meta.Updater.PublicKey)
		if err != nil {
			return fail(diag.CodeNativeUpdaterKey, "native.updater.publicKey", err.Error(), "copy the public key printed by krate native keygen")
		}
		if !pub.Equal(o.UpdaterKey.Public()) {
			return fail(diag.CodeNativeUpdaterKey, EnvUpdaterKey, "the private key does not match native.updater.publicKey",
				"use the private key that krate native keygen wrote together with that public key")
		}
	}
	for i := range res.Artifacts {
		a := &res.Artifacts[i]
		if !a.Update {
			continue
		}
		f, err := os.Open(a.Path)
		if err != nil {
			return wrapWrite(a.Path, err)
		}
		sig, err := updater.Sign(o.UpdaterKey, updater.Meta{AppID: o.Meta.AppID, Target: o.Payload.OS + "-" + o.Payload.Arch, Version: o.Meta.Version}, f)
		st, _ := f.Stat()
		f.Close()
		if err != nil {
			return wrapWrite(a.Path, err)
		}
		a.Signature = sig
		url := filepath.Base(a.Path)
		if o.UpdateBaseURL != "" {
			url = strings.TrimRight(o.UpdateBaseURL, "/") + "/" + url
		}
		feed := updater.Feed{
			Version: o.Meta.Version, Notes: o.Notes, PubDate: buildTime().Format(time.RFC3339),
			URL: url, Signature: sig, Size: st.Size(),
			Rollout: o.Rollout, Critical: o.Critical, MinVersion: o.MinVersion,
		}
		patches, err := generatePatches(o, res, a, st.Size())
		if err != nil {
			return err
		}
		feed.Patches = patches
		if err := feed.Validate(); err != nil {
			return fail(diag.CodeNativeUpdaterKey, "update feed", err.Error(), "check --rollout, --rollout-start, --rollout-ramp-hours and --min-version")
		}
		doc, err := json.MarshalIndent(feed, "", "  ")
		if err != nil {
			return err
		}
		out := filepath.Join(o.OutDir, feedName(o.Channel, o.Payload.OS, o.Payload.Arch))
		if err := os.WriteFile(out, append(doc, '\n'), 0o644); err != nil {
			return wrapWrite(out, err)
		}
		res.add(Artifact{Path: out, Format: "update-feed"})
	}
	return nil
}

// feedName is latest-<os>-<arch>.json, or latest-<channel>-<os>-<arch>.json
// for a named channel.
func feedName(channel, goos, goarch string) string {
	if channel != "" {
		return fmt.Sprintf("latest-%s-%s-%s.json", channel, goos, goarch)
	}
	return fmt.Sprintf("latest-%s-%s.json", goos, goarch)
}

// exeName is the executable's name inside the update file.
func (o *Options) exeName() string {
	if o.Payload.OS == "windows" {
		return winExeName(o.Meta)
	}
	return o.Meta.slug()
}

// previousArtifacts finds update files of older releases in the DeltaFrom
// locations, newest first, at most count.
func previousArtifacts(o *Options, current string, count int) []struct{ Version, Path string } {
	ext := ".tar.gz"
	if o.Payload.OS != "linux" {
		ext = ".zip"
	}
	prefix := o.Meta.slug() + "-"
	suffix := "-" + o.Payload.OS + "-" + o.Payload.Arch + ext
	var found []struct{ Version, Path string }
	seen := map[string]bool{}
	add := func(p string) {
		base := filepath.Base(p)
		if !strings.HasPrefix(base, prefix) || !strings.HasSuffix(base, suffix) {
			return
		}
		v := strings.TrimSuffix(strings.TrimPrefix(base, prefix), suffix)
		if c, ok := updater.CompareVersions(v, current); !ok || c >= 0 || seen[v] {
			return
		}
		seen[v] = true
		found = append(found, struct{ Version, Path string }{v, p})
	}
	for _, loc := range o.DeltaFrom {
		st, err := os.Stat(loc)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			add(loc)
			continue
		}
		_ = filepath.WalkDir(loc, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				add(p)
			}
			return nil
		})
	}
	sort.Slice(found, func(i, j int) bool {
		c, _ := updater.CompareVersions(found[i].Version, found[j].Version)
		return c > 0
	})
	if len(found) > count {
		found = found[:count]
	}
	return found
}

// generatePatches writes a signed patch from each recent release to this one
// and returns the feed entries. A release that cannot be patched is skipped
// with a note: the full download is always there.
func generatePatches(o *Options, res *Result, a *Artifact, fullSize int64) ([]updater.FeedPatch, error) {
	if len(o.DeltaFrom) == 0 {
		return nil, nil
	}
	count := o.DeltaCount
	if count <= 0 {
		count = 3
	}
	kind := updater.KindTree
	if o.Payload.OS == "darwin" {
		kind = updater.KindBundle
	}
	target := o.Payload.OS + "-" + o.Payload.Arch
	prevs := previousArtifacts(o, o.Meta.Version, count)
	if len(prevs) == 0 {
		res.note("--delta-from found no earlier %s update files for %s (named %s)", target, o.Meta.slug(), artifactName(o.Meta, Payload{OS: o.Payload.OS, Arch: o.Payload.Arch}, "")+"...")
	}
	var out []updater.FeedPatch
	for _, pv := range prevs {
		name := fmt.Sprintf("%s-%s-to-%s-%s-%s.krpatch", o.Meta.slug(), pv.Version, o.Meta.Version, o.Payload.OS, o.Payload.Arch)
		dst := filepath.Join(o.OutDir, name)
		info, err := updater.GeneratePatch(pv.Path, a.Path, dst, updater.PatchOptions{
			Kind: kind, ExeName: o.exeName(), Key: o.UpdaterKey,
			Meta: updater.PatchMeta{AppID: o.Meta.AppID, Target: target, From: pv.Version, To: o.Meta.Version},
		})
		if err != nil {
			res.note("no patch from %s: %v", pv.Version, err)
			continue
		}
		if info.Size >= fullSize {
			os.Remove(dst)
			res.note("no patch from %s: it would not be smaller than the full file (%d >= %d bytes)", pv.Version, info.Size, fullSize)
			continue
		}
		url := name
		if o.UpdateBaseURL != "" {
			url = strings.TrimRight(o.UpdateBaseURL, "/") + "/" + name
		}
		res.add(Artifact{Path: dst, Format: "patch", Detail: fmt.Sprintf("delta update from %s, %.1f%% of the full file", pv.Version, 100*float64(info.Size)/float64(fullSize))})
		out = append(out, updater.FeedPatch{From: pv.Version, URL: url, Signature: info.Signature, Size: info.Size, SHA512: info.SHA512})
	}
	return out, nil
}

// FeedEdit changes the unsigned rollout controls of a written feed. The
// signatures stay valid: they cover the files, not these fields.
type FeedEdit struct {
	// Rollout replaces the rollout; ClearRollout removes it (everybody).
	Rollout      *updater.Rollout
	ClearRollout bool
	// Critical and MinVersion are left alone when nil.
	Critical   *bool
	MinVersion *string
}

// EditFeed applies edit to the feed file at path.
func EditFeed(path string, edit FeedEdit) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return wrapWrite(path, err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fail(diag.CodeNativeUpdaterKey, path, "not a JSON object: "+err.Error(), "")
	}
	set := func(k string, v any, drop bool) error {
		if drop {
			delete(doc, k)
			return nil
		}
		b, err := json.Marshal(v)
		doc[k] = b
		return err
	}
	if edit.ClearRollout {
		_ = set("rollout", nil, true)
	}
	if edit.Rollout != nil {
		if err := set("rollout", edit.Rollout, false); err != nil {
			return err
		}
	}
	if edit.Critical != nil {
		if err := set("critical", true, !*edit.Critical); err != nil {
			return err
		}
	}
	if edit.MinVersion != nil {
		if err := set("min_version", *edit.MinVersion, *edit.MinVersion == ""); err != nil {
			return err
		}
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if _, err := updater.ParseFeed(out); err != nil {
		return fail(diag.CodeNativeUpdaterKey, path, err.Error(), "")
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return wrapWrite(path, err)
	}
	return nil
}

// WritePatch writes one signed patch between two update files; it is what
// `krate native patch` runs, for artifacts the packager does not feed (an
// AppImage). It returns the feed entry to add to a feed by hand.
func WritePatch(oldFile, newFile, out string, kind, exeName string, meta updater.PatchMeta, key ed25519.PrivateKey, url string) (*updater.FeedPatch, error) {
	info, err := updater.GeneratePatch(oldFile, newFile, out, updater.PatchOptions{Kind: kind, ExeName: exeName, Meta: meta, Key: key})
	if err != nil {
		return nil, fail(diag.CodeNativePkgInput, out, err.Error(), "")
	}
	if url == "" {
		url = filepath.Base(out)
	}
	return &updater.FeedPatch{From: meta.From, URL: url, Signature: info.Signature, Size: info.Size, SHA512: info.SHA512}, nil
}
