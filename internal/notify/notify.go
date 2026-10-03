// Package notify sends desktop notifications through the freedesktop
// org.freedesktop.Notifications D-Bus service (GNOME, KDE, …).
package notify

import (
	"sync"

	"github.com/godbus/dbus/v5"
)

// Urgency levels per the freedesktop spec.
const (
	Low      byte = 0
	Normal   byte = 1
	Critical byte = 2
)

// Notifier sends notifications; safe for concurrent use.
type Notifier struct {
	mx   sync.Mutex
	conn *dbus.Conn
	app  string
}

// New connects to the session bus lazily.
func New(app string) *Notifier { return &Notifier{app: app} }

// Send shows a notification. Errors are returned, never fatal.
func (n *Notifier) Send(summary, body string, urgency byte) error {
	n.mx.Lock()
	defer n.mx.Unlock()
	if n.conn == nil {
		c, err := dbus.ConnectSessionBus()
		if err != nil {
			return err
		}
		n.conn = c
	}
	obj := n.conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	hints := map[string]dbus.Variant{"urgency": dbus.MakeVariant(urgency)}
	call := obj.Call("org.freedesktop.Notifications.Notify", 0,
		n.app, uint32(0), "", summary, body, []string{}, hints, int32(6000))
	if call.Err != nil {
		_ = n.conn.Close()
		n.conn = nil
	}
	return call.Err
}
