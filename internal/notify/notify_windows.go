package notify

import (
	"os/exec"
	"strings"
	"syscall"
)

// Notifier sends Windows toast notifications through PowerShell and the
// WinRT notification API (Windows 10+); safe for concurrent use.
type Notifier struct{ app string }

// New returns a notifier for app.
func New(app string) *Notifier { return &Notifier{app: app} }

// powershellAUMID is the AppUserModelID of Windows PowerShell, registered on
// every Windows install: toasts from unregistered AUMIDs are dropped.
const powershellAUMID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Send shows a notification. Errors are returned, never fatal.
func (n *Notifier) Send(summary, body string, urgency byte) error {
	scenario := ""
	if urgency == Critical {
		scenario = ` scenario="urgent"`
	}
	xml := `<toast` + scenario + `><visual><binding template="ToastGeneric"><text>` + xmlEscape(n.app+" · "+summary) +
		`</text><text>` + xmlEscape(body) + `</text></binding></visual></toast>`
	script := `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null;` +
		`[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] > $null;` +
		`$x = New-Object Windows.Data.Xml.Dom.XmlDocument; $x.LoadXml(` + psQuote(xml) + `);` +
		`[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier(` + psQuote(powershellAUMID) +
		`).Show([Windows.UI.Notifications.ToastNotification]::new($x))`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd.Run()
}
