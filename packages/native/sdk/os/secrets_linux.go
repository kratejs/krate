//go:build linux

package sdkos

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/internal/dbus"
	"github.com/kratejs/krate/packages/native/krate"
)

func platformBackend() secretBackend {
	return &secretService{dial: dbus.Dial}
}

// Secret Service (org.freedesktop.Secret) names.
const (
	ssName    = "org.freedesktop.secrets"
	ssPath    = dbus.ObjectPath("/org/freedesktop/secrets")
	ssService = "org.freedesktop.Secret.Service"
	ssColl    = "org.freedesktop.Secret.Collection"
	ssItem    = "org.freedesktop.Secret.Item"
	ssSession = "org.freedesktop.Secret.Session"
	ssPrompt  = "org.freedesktop.Secret.Prompt"
	noObject  = dbus.ObjectPath("/")

	itemLabelProp = "org.freedesktop.Secret.Item.Label"
	itemAttrsProp = "org.freedesktop.Secret.Item.Attributes"
)

// promptTimeout bounds how long the user may take to answer an unlock prompt.
const promptTimeout = 2 * time.Minute

// secretService talks to a Secret Service provider (gnome-keyring, KWallet,
// KeePassXC) over the session bus using a plain (unencrypted) session; the
// bus is private to the user.
type secretService struct {
	dial func(context.Context) (*dbus.Conn, error)
}

// session is one connection with an open Secret Service session.
type ssConn struct {
	c       *dbus.Conn
	session dbus.ObjectPath
}

func (s *secretService) open(ctx context.Context) (*ssConn, error) {
	c, err := s.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("sdkos: no session bus for the Secret Service (%v): %w", err, krate.ErrUnsupported)
	}
	res, err := c.Call(ctx, ssName, ssPath, ssService, "OpenSession", "sv", "plain", dbus.V("s", ""))
	if err != nil {
		c.Close()
		return nil, mapSSError(err)
	}
	if len(res) != 2 {
		c.Close()
		return nil, errors.New("sdkos: bad OpenSession reply")
	}
	sp, _ := res[1].(dbus.ObjectPath)
	return &ssConn{c: c, session: sp}, nil
}

func (sc *ssConn) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sc.c.Call(ctx, ssName, sc.session, ssSession, "Close", "")
	sc.c.Close()
}

// mapSSError turns "no provider" bus errors into ErrUnsupported.
func mapSSError(err error) error {
	var de *dbus.Error
	if errors.As(err, &de) {
		switch de.Name {
		case "org.freedesktop.DBus.Error.ServiceUnknown", "org.freedesktop.DBus.Error.NameHasNoOwner",
			"org.freedesktop.DBus.Error.NoReply", "org.freedesktop.DBus.Error.Spawn.ServiceNotFound":
			return fmt.Errorf("sdkos: no Secret Service provider is running (install gnome-keyring, KWallet or KeePassXC): %w", krate.ErrUnsupported)
		}
	}
	return fmt.Errorf("sdkos: Secret Service: %w", err)
}

func attrs(service, key string) map[string]string {
	return map[string]string{"service": service, "username": key}
}

// prompt runs a Secret Service prompt object and waits for its Completed
// signal. It returns an error when the user dismisses it.
func (sc *ssConn) prompt(ctx context.Context, p dbus.ObjectPath) error {
	if p == noObject || p == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, promptTimeout)
	defer cancel()
	done := make(chan bool, 1) // true when dismissed
	unsub, err := sc.c.Subscribe(ctx, dbus.Match{Path: p, Interface: ssPrompt, Member: "Completed"}, func(m *dbus.Message) {
		dismissed := false
		if len(m.Body) > 0 {
			dismissed, _ = m.Body[0].(bool)
		}
		select {
		case done <- dismissed:
		default:
		}
	})
	if err != nil {
		return mapSSError(err)
	}
	defer unsub()
	if _, err := sc.c.Call(ctx, ssName, p, ssPrompt, "Prompt", "s", ""); err != nil {
		return mapSSError(err)
	}
	select {
	case dismissed := <-done:
		if dismissed {
			return errors.New("sdkos: the keyring prompt was dismissed")
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("sdkos: waiting for the keyring prompt: %w", ctx.Err())
	}
}

// unlock unlocks objects, prompting when the provider asks.
func (sc *ssConn) unlock(ctx context.Context, objs []dbus.ObjectPath) error {
	if len(objs) == 0 {
		return nil
	}
	res, err := sc.c.Call(ctx, ssName, ssPath, ssService, "Unlock", "ao", objs)
	if err != nil {
		return mapSSError(err)
	}
	if len(res) == 2 {
		p, _ := res[1].(dbus.ObjectPath)
		return sc.prompt(ctx, p)
	}
	return nil
}

func (sc *ssConn) defaultCollection(ctx context.Context) (dbus.ObjectPath, error) {
	res, err := sc.c.Call(ctx, ssName, ssPath, ssService, "ReadAlias", "s", "default")
	if err != nil {
		return "", mapSSError(err)
	}
	p, _ := res[0].(dbus.ObjectPath)
	if p == noObject || p == "" {
		return "", fmt.Errorf("sdkos: the Secret Service has no default keyring: %w", krate.ErrUnsupported)
	}
	return p, nil
}

// find returns the items for (service, key), unlocked first, with any locked
// ones unlocked.
func (sc *ssConn) find(ctx context.Context, service, key string) ([]dbus.ObjectPath, error) {
	res, err := sc.c.Call(ctx, ssName, ssPath, ssService, "SearchItems", "a{ss}", attrs(service, key))
	if err != nil {
		return nil, mapSSError(err)
	}
	if len(res) != 2 {
		return nil, errors.New("sdkos: bad SearchItems reply")
	}
	unlocked, _ := res[0].([]dbus.ObjectPath)
	locked, _ := res[1].([]dbus.ObjectPath)
	if len(locked) > 0 {
		if err := sc.unlock(ctx, locked); err != nil {
			return nil, err
		}
	}
	return slices.Concat(unlocked, locked), nil
}

func (s *secretService) set(ctx context.Context, service, key, value string) error {
	sc, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer sc.close()
	coll, err := sc.defaultCollection(ctx)
	if err != nil {
		return err
	}
	if err := sc.unlock(ctx, []dbus.ObjectPath{coll}); err != nil {
		return err
	}
	props := map[string]dbus.Variant{
		itemLabelProp: dbus.V("s", service+": "+key),
		itemAttrsProp: dbus.V("a{ss}", attrs(service, key)),
	}
	secret := []any{sc.session, []byte{}, []byte(value), "text/plain"}
	res, err := sc.c.Call(ctx, ssName, coll, ssColl, "CreateItem", "a{sv}(oayays)b", props, secret, true)
	if err != nil {
		return mapSSError(err)
	}
	if len(res) == 2 {
		p, _ := res[1].(dbus.ObjectPath)
		return sc.prompt(ctx, p)
	}
	return nil
}

func (s *secretService) get(ctx context.Context, service, key string) (string, error) {
	sc, err := s.open(ctx)
	if err != nil {
		return "", err
	}
	defer sc.close()
	items, err := sc.find(ctx, service, key)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", ErrSecretNotFound
	}
	res, err := sc.c.Call(ctx, ssName, items[0], ssItem, "GetSecret", "o", sc.session)
	if err != nil {
		return "", mapSSError(err)
	}
	st, _ := res[0].([]any)
	if len(st) != 4 {
		return "", errors.New("sdkos: bad GetSecret reply")
	}
	v, _ := st[2].([]byte)
	return string(v), nil
}

func (s *secretService) remove(ctx context.Context, service, key string) error {
	sc, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer sc.close()
	items, err := sc.find(ctx, service, key)
	if err != nil {
		return err
	}
	for _, it := range items {
		res, err := sc.c.Call(ctx, ssName, it, ssItem, "Delete", "")
		if err != nil {
			return mapSSError(err)
		}
		if len(res) == 1 {
			p, _ := res[0].(dbus.ObjectPath)
			if err := sc.prompt(ctx, p); err != nil {
				return err
			}
		}
	}
	return nil
}

var (
	probeMu   sync.Mutex
	probeAt   time.Time
	probeLast bool
)

// secretsAvailable reports whether a Secret Service provider owns or can be
// started for its bus name. The answer is cached for 15 seconds.
func secretsAvailable() bool {
	probeMu.Lock()
	defer probeMu.Unlock()
	if !probeAt.IsZero() && time.Since(probeAt) < 15*time.Second {
		return probeLast
	}
	ok := probeSecrets()
	probeAt, probeLast = time.Now(), ok
	return ok
}

func probeSecrets() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dbus.Dial(ctx)
	if err != nil {
		return false
	}
	defer c.Close()
	if res, err := c.Call(ctx, dbus.BusName, dbus.BusPath, dbus.BusInterface, "NameHasOwner", "s", ssName); err == nil && len(res) == 1 {
		if has, _ := res[0].(bool); has {
			return true
		}
	}
	if res, err := c.Call(ctx, dbus.BusName, dbus.BusPath, dbus.BusInterface, "ListActivatableNames", ""); err == nil && len(res) == 1 {
		names, _ := res[0].([]string)
		return slices.Contains(names, ssName)
	}
	return false
}
