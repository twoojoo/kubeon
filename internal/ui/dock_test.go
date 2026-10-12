package ui

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
)

type fakeDockTab struct{}

func (fakeDockTab) Title() string      { return "fake" }
func (fakeDockTab) Icon() *widget.Icon { return icClose }
func (fakeDockTab) Layout(gtx C) D     { return D{Size: gtx.Constraints.Max} }
func (fakeDockTab) Close()             {}
func (fakeDockTab) Focused(gtx C) bool { return false }

// TestDockDragFollowsPointer drags the dock's top edge with several drag
// events per frame (as a fast mouse does): the dock top must end exactly
// under the pointer, keeping the offset at which the grip was grabbed.
func TestDockDragFollowsPointer(t *testing.T) {
	const winH = 1000
	d := newDock(&App{th: newTheme()})
	d.add(fakeDockTab{})
	var r input.Router
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{
			Ops:         ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(800, winH)),
			Source:      r.Source(),
		}
		// as in App: content above, the dock at the bottom
		layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D { return D{Size: gtx.Constraints.Max} }),
			layout.Rigid(d.Layout),
		)
		r.Frame(ops)
	}
	frame()
	frame()
	top := d.top // default height
	at := func(k pointer.Kind, y float32) pointer.Event {
		return pointer.Event{Kind: k, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(400, y)}
	}
	grab := float32(top) + 2 // inside the 5px grip, 2px below the dock top
	r.Queue(at(pointer.Press, grab))
	frame()
	if !d.dragging {
		t.Fatal("press on the grip did not start a drag")
	}
	for _, y := range []float32{grab - 40, grab - 90, grab - 150, grab + 30, grab - 210} {
		// several moves hit-tested against the same frame
		r.Queue(at(pointer.Move, y-10), at(pointer.Move, y-5), at(pointer.Move, y))
		frame()
		if want := int(y) - 2; d.top != want {
			t.Fatalf("pointer at %v: dock top %d, want %d", y, d.top, want)
		}
	}
	r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(400, grab-210)})
	frame()
	if d.dragging {
		t.Fatal("still dragging after release")
	}
	if got, want := d.curH, winH-(top-210); got != want {
		t.Fatalf("height %d, want %d", got, want)
	}
}
