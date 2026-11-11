package fakeplat

import (
	"sync"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Desktop is an App whose platform layer also implements the tray and
// notification capabilities itself (as the macOS layer does), so the SDK uses
// those instead of the Linux D-Bus services.
type Desktop struct {
	*App

	mu        sync.Mutex
	tray      *caps.TrayIcon
	trayCalls int
	trayFn    func(caps.TrayEvent)

	unavailable error
	notified    []caps.Notification
	closed      []string
	notifyFn    func(caps.NotifyEvent)
}

var (
	_ caps.Tray                 = (*Desktop)(nil)
	_ caps.Notifier             = (*Desktop)(nil)
	_ caps.NotifierAvailability = (*Desktop)(nil)
	_ caps.NotificationCloser   = (*Desktop)(nil)
)

// NewDesktop returns a Desktop.
func NewDesktop() *Desktop { return &Desktop{App: New()} }

func (d *Desktop) SetTray(icon *caps.TrayIcon) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.trayCalls++
	d.tray = icon
	return nil
}

func (d *Desktop) OnTray(fn func(caps.TrayEvent)) { d.mu.Lock(); d.trayFn = fn; d.mu.Unlock() }

// EmitTray delivers a tray event.
func (d *Desktop) EmitTray(ev caps.TrayEvent) {
	d.mu.Lock()
	fn := d.trayFn
	d.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

func (d *Desktop) NotifyAvailability() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.unavailable
}

func (d *Desktop) Notify(n caps.Notification) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.unavailable != nil {
		return d.unavailable
	}
	d.notified = append(d.notified, n)
	return nil
}

func (d *Desktop) CloseNotification(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = append(d.closed, id)
	return nil
}

func (d *Desktop) OnNotify(fn func(caps.NotifyEvent)) { d.mu.Lock(); d.notifyFn = fn; d.mu.Unlock() }

// EmitNotify delivers a notification event.
func (d *Desktop) EmitNotify(ev caps.NotifyEvent) {
	d.mu.Lock()
	fn := d.notifyFn
	d.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// TrayState returns the current tray icon (nil when none) and how often
// SetTray was called.
func (d *Desktop) TrayState() (*caps.TrayIcon, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.tray, d.trayCalls
}

// Notified returns the notifications posted so far.
func (d *Desktop) Notified() []caps.Notification {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]caps.Notification(nil), d.notified...)
}

// Closed returns the ids passed to CloseNotification.
func (d *Desktop) Closed() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.closed...)
}

// SetUnavailable makes NotifyAvailability and Notify return err (nil restores
// normal behavior).
func (d *Desktop) SetUnavailable(err error) {
	d.mu.Lock()
	d.unavailable = err
	d.mu.Unlock()
}
