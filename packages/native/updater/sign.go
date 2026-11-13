package updater

import (
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// MessagePrefix starts every signed update message and versions its format.
const MessagePrefix = "krate-update-v1"

// Meta is what a signature binds an artifact to besides its bytes.
type Meta struct {
	// AppID is the app identifier (native.appId).
	AppID string
	// Target is "<goos>-<goarch>", for example "linux-amd64".
	Target string
	// Version is the release version exactly as the feed reports it.
	Version string
}

// message builds the canonical signed message:
//
//	krate-update-v1\n<appId>\n<target>\n<version>\n<sha512 hex>\n<size>
func (m Meta) message(sum []byte, size int64) ([]byte, error) {
	for _, f := range []string{m.AppID, m.Target, m.Version} {
		if f == "" || strings.ContainsAny(f, "\r\n") {
			return nil, errors.New("updater: appId, target and version must be non-empty single-line strings")
		}
	}
	return []byte(MessagePrefix + "\n" + m.AppID + "\n" + m.Target + "\n" + m.Version + "\n" +
		hex.EncodeToString(sum) + "\n" + strconv.FormatInt(size, 10)), nil
}

func digest(r io.Reader) (sum []byte, size int64, err error) {
	h := sha512.New()
	size, err = io.Copy(h, r)
	if err != nil {
		return nil, 0, err
	}
	return h.Sum(nil), size, nil
}

// Sign returns the base64 Ed25519 signature of the canonical message for the
// data read from r (hashed as a stream) and its metadata.
func Sign(priv ed25519.PrivateKey, m Meta, r io.Reader) (string, error) {
	sum, size, err := digest(r)
	if err != nil {
		return "", err
	}
	msg, err := m.message(sum, size)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg)), nil
}

// Verify checks a base64 signature of the data read from r against the
// expected metadata. Any difference in app id, target, version, size or bytes
// yields ErrBadSignature.
func Verify(pub ed25519.PublicKey, m Meta, r io.Reader, signature string) error {
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	sum, size, err := digest(r)
	if err != nil {
		return err
	}
	msg, err := m.message(sum, size)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadSignature, err)
	}
	if !ed25519.Verify(pub, msg, sig) {
		return ErrBadSignature
	}
	return nil
}

func (u *Update) meta() Meta {
	return Meta{AppID: u.cfg.AppID, Target: u.cfg.target(), Version: u.Version}
}
