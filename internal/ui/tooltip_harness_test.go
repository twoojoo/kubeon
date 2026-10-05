//go:build dev && integration

package ui

import (
	"image"
	"testing"
	"time"
)

func TestUITruncatedCellTooltip(t *testing.T) {
	isolatedPrefs(t)
	h := newHarness(t, Options{Namespace: "demo", Resource: "po"})
	a, v := h.a, h.a.view
	h.waitFor("pods", func() bool { return v.snap != nil && len(v.snap.Rows) > 0 })
	h.press("D") // the drawer narrows the table: long cells get an ellipsis
	h.settle(150 * time.Millisecond)
	col := -1
	for i, c := range v.snap.Columns {
		if c.Name == "NODE" {
			col = i
		}
	}
	if col < 0 {
		t.Fatal("no NODE column")
	}
	x := 0
	for i := 0; i < col; i++ {
		x += v.widths[i]
	}
	full := v.snap.Rows[0].Cells[col]
	v.hover, v.hoverX = 0, x+v.widths[col]/2
	a.pointer = image.Pt(900, 200)
	h.frame()
	if a.tip.key == "" {
		t.Fatalf("no tooltip offered for %q in a %d px column", full, v.widths[col])
	}
	time.Sleep(tipDelay + 50*time.Millisecond)
	h.frame()
	if a.tip.text != full {
		t.Fatalf("tooltip %q, want %q", a.tip.text, full)
	}
	h.shot("63-cell-tooltip")

	// a cell that fits offers nothing
	v.hoverX = 2
	h.frame()
	h.frame()
	if a.tip.key != "" && a.tip.text == v.snap.Rows[0].Cells[0] {
		t.Fatal("tooltip offered for a cell that fits")
	}
}
