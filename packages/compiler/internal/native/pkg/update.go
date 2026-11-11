package pkg

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		sig, err := updater.Sign(o.UpdaterKey, f)
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
		}
		doc, err := json.MarshalIndent(feed, "", "  ")
		if err != nil {
			return err
		}
		out := filepath.Join(o.OutDir, fmt.Sprintf("latest-%s-%s.json", o.Payload.OS, o.Payload.Arch))
		if err := os.WriteFile(out, append(doc, '\n'), 0o644); err != nil {
			return wrapWrite(out, err)
		}
		res.add(Artifact{Path: out, Format: "update-feed"})
	}
	return nil
}
