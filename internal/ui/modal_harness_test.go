//go:build dev && integration

package ui

import (
	"testing"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

func (h *harness) click(x, y float32) {
	p := f32.Pt(x, y)
	h.events(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p},
	)
}

func TestModalDismissOnBackdrop(t *testing.T) {
	h := newHarness(t, Options{Namespace: "demo", Resource: "po"})
	a := h.a
	h.waitFor("pods", func() bool { return a.view.snap != nil && len(a.view.snap.Rows) > 0 })

	for _, open := range []func(){
		func() { h.press(":", key.ModShift) },   // command palette
		func() { a.view.confirmDelete() },       // confirm dialog
		func() { a.overlay = newNotifPanel(a) }, // notifications
	} {
		open()
		h.waitFor("overlay open", func() bool { return a.overlay != nil })
		// inside the dialog, on padding (title row area): must stay open
		h.click(float32(h.size.X)/2, 100)
		if a.overlay == nil {
			t.Fatal("click inside dialog closed it")
		}
		// outside: bottom-left corner of the backdrop
		h.click(40, float32(h.size.Y)-60)
		h.waitFor("overlay closed", func() bool { return a.overlay == nil })
	}
}
