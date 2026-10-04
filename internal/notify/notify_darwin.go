package notify

import (
	"os/exec"
	"strconv"
)

// Notifier sends notifications through Notification Center; safe for
// concurrent use.
type Notifier struct{ app string }

// New returns a notifier for app.
func New(app string) *Notifier { return &Notifier{app: app} }

// Send shows a notification. Errors are returned, never fatal.
func (n *Notifier) Send(summary, body string, urgency byte) error {
	// strconv.Quote yields a valid AppleScript string literal for the
	// characters that matter here (quotes, backslashes, newlines).
	script := "display notification " + strconv.Quote(body) +
		" with title " + strconv.Quote(n.app) + " subtitle " + strconv.Quote(summary)
	if urgency == Critical {
		script += ` sound name "Basso"`
	}
	return exec.Command("osascript", "-e", script).Run()
}
