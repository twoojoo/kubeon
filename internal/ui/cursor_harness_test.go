//go:build dev && integration

package ui

import (
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
)

// Clickable things show the hand cursor, plain text doesn't.
func TestUIPointerCursor(t *testing.T) {
	isolatedPrefs(t)
	h := newHarness(t, Options{Namespace: "demo", Resource: "po"})
	v := h.a.view
	h.waitFor("pods", func() bool { return v.snap != nil && len(v.snap.Rows) > 3 })
	at := func(x, y float32) pointer.Cursor {
		h.events(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(x, y)})
		return h.r.Cursor()
	}
	// a table row: scan down the middle of the table until one shows the hand
	rowHand := false
	for y := float32(120); y < 400 && !rowHand; y += 8 {
		rowHand = at(700, y) == pointer.CursorPointer && v.hover >= 0
	}
	if !rowHand {
		t.Error("table rows don't show the hand cursor")
	}
	// a sidebar entry (Pods, under Workloads)
	hand := false
	for y := float32(100); y < 500 && !hand; y += 8 {
		hand = at(80, y) == pointer.CursorPointer
	}
	if !hand {
		t.Error("sidebar entries don't show the hand cursor")
	}
	// plain text in the status bar (context name)
	if c := at(30, float32(h.size.Y-12)); c == pointer.CursorPointer {
		t.Errorf("status bar text shows %v", c)
	}
}
