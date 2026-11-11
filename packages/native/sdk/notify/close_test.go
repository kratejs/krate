package notify

import (
	"errors"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

type plainNotifier struct{}

func (plainNotifier) Notify(caps.Notification) error  { return nil }
func (plainNotifier) OnNotify(func(caps.NotifyEvent)) {}

type closingNotifier struct {
	plainNotifier
	closed []string
}

func (c *closingNotifier) CloseNotification(id string) error {
	c.closed = append(c.closed, id)
	return nil
}

func TestPlatformBackendClose(t *testing.T) {
	if err := (platformBackend{plainNotifier{}}).close("x"); !errors.Is(err, krate.ErrUnsupported) {
		t.Errorf("a notifier that cannot close must report unsupported, got %v", err)
	}
	c := &closingNotifier{}
	if err := (platformBackend{c}).close("n1"); err != nil || len(c.closed) != 1 || c.closed[0] != "n1" {
		t.Errorf("close = %v, %v", err, c.closed)
	}
}
