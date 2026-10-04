//go:build dev && integration

package ui

import (
	"testing"
	"time"
)

// TestUIMacTitlebar renders the titlebar as on macOS: no custom window
// buttons, brand shifted right of the native traffic lights.
func TestUIMacTitlebar(t *testing.T) {
	isolatedPrefs(t)
	nativeWindowControls = true
	t.Cleanup(func() { nativeWindowControls = false })
	h := newHarness(t, Options{Resource: "v1/pods", Namespace: "demo"})
	h.waitFor("pods", func() bool { return h.a.view != nil && h.a.view.snap != nil && len(h.a.view.snap.Rows) > 0 })
	h.settle(300 * time.Millisecond)
	h.shot("40-macos-titlebar")
}
