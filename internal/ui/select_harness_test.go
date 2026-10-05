//go:build dev && integration

package ui

import (
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

func TestUISelectableText(t *testing.T) {
	isolatedPrefs(t)
	h := newHarness(t, Options{Namespace: "demo", Resource: "po"})
	a := h.a
	selectRow(t, h, "demo/web-")

	// structured details: values are selectable texts
	h.press("D")
	a.drawer.tab = tabOverview
	h.waitFor("details", func() bool { return a.drawer.details != nil })
	h.settle(150 * time.Millisecond)
	n := 0
	for k := range a.sels {
		if strings.HasPrefix(k, "det/") {
			n++
		}
	}
	if n == 0 {
		t.Fatal("no selectable texts in the details view")
	}

	// YAML: drag across lines in the drawer selects them, Ctrl+C copies
	a.drawer.tab = tabYAML
	h.waitFor("yaml", func() bool { return len(a.drawer.yamlV.raw) > 20 })
	h.settle(150 * time.Millisecond)
	x := float32(h.size.X - 220)
	h.events(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(x, 300)},
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(x, 360)},
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(x, 420)},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(x, 420)},
	)
	s := a.drawer.yamlV.sel
	if !s.active() {
		t.Fatal("drag did not select YAML lines")
	}
	lo, hi := s.bounds()
	if hi-lo < 2 {
		t.Fatalf("selected lines %d..%d", lo, hi)
	}
	h.shot("64-yaml-line-select")
	h.press("C", key.ModShortcut)
	if !strings.HasPrefix(a.status, "Copied ") {
		t.Fatalf("status %q", a.status)
	}
	h.press(key.NameEscape)
	if s.active() {
		t.Fatal("esc should clear the line selection")
	}
}
