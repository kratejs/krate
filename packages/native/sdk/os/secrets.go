package sdkos

import (
	"context"
	"errors"
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
)

// ErrSecretNotFound is returned by SecretGet when the key has no secret.
var ErrSecretNotFound = errors.New("sdkos: secret not found")

// secretBackend is one OS credential store. service is the appId.
type secretBackend interface {
	set(ctx context.Context, service, key, value string) error
	get(ctx context.Context, service, key string) (string, error)
	// remove deletes a secret; a missing one is not an error.
	remove(ctx context.Context, service, key string) error
}

// backend is the platform store: the Secret Service on Linux, the Keychain on
// macOS and the Credential Manager on Windows; nil elsewhere.
var backend = platformBackend()

const maxSecretKey, maxSecretValue = 256, 1 << 20

func checkKey(key string) error {
	if key == "" || len(key) > maxSecretKey {
		return krate.Errorf("bad_args", "secret key must be 1 to %d bytes", maxSecretKey)
	}
	for _, r := range key {
		if r == 0 {
			return krate.Errorf("bad_args", "secret key contains a NUL byte")
		}
	}
	return nil
}

func store() (secretBackend, App, error) {
	a, err := Current()
	if err != nil {
		return nil, a, err
	}
	if backend == nil {
		return nil, a, fmt.Errorf("sdkos: no credential store on this platform: %w", krate.ErrUnsupported)
	}
	return backend, a, nil
}

// SecretSet stores value under key for this app in the OS credential store.
// It returns krate.ErrUnsupported when the platform has no store (for
// example Linux without a Secret Service provider such as gnome-keyring or
// KWallet). Windows limits a value to about 2.5 KB.
func SecretSet(ctx context.Context, key, value string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if len(value) > maxSecretValue {
		return krate.Errorf("bad_args", "secret value is larger than %d bytes", maxSecretValue)
	}
	b, a, err := store()
	if err != nil {
		return err
	}
	return b.set(ctx, a.ID, key, value)
}

// SecretGet returns the secret stored under key, or ErrSecretNotFound.
func SecretGet(ctx context.Context, key string) (string, error) {
	if err := checkKey(key); err != nil {
		return "", err
	}
	b, a, err := store()
	if err != nil {
		return "", err
	}
	return b.get(ctx, a.ID, key)
}

// SecretDelete removes the secret under key. A missing key is not an error.
func SecretDelete(ctx context.Context, key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	b, a, err := store()
	if err != nil {
		return err
	}
	return b.remove(ctx, a.ID, key)
}
