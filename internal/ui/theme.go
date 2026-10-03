package ui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/giovanni/k9desk/internal/backend"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

// Palette is a Lens-like dark palette.
type Palette struct {
	Bg, Sidebar, Panel, Header, Border, Hover, Selected    color.NRGBA
	Text, TextDim, TextBright, Accent, Ok, Warn, Err, Info color.NRGBA
}

var pal = Palette{
	Bg:         rgb(0x1e2124),
	Sidebar:    rgb(0x252a2e),
	Panel:      rgb(0x2b3035),
	Header:     rgb(0x262b2f),
	Border:     rgb(0x363c42),
	Hover:      rgb(0x30363c),
	Selected:   rgb(0x2f4a63),
	Text:       rgb(0xb8bcc2),
	TextDim:    rgb(0x7d848c),
	TextBright: rgb(0xeceef1),
	Accent:     rgb(0x3d90ce),
	Ok:         rgb(0x4caf50),
	Warn:       rgb(0xff9800),
	Err:        rgb(0xe85555),
	Info:       rgb(0x5fb3e8),
}

func rgb(c uint32) color.NRGBA {
	return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
}

func stateColor(s backend.RowState) color.NRGBA {
	switch s {
	case backend.StateAdd:
		return pal.Info
	case backend.StateMod:
		return pal.Text
	case backend.StatePending:
		return pal.Warn
	case backend.StateErr:
		return pal.Err
	case backend.StateKill:
		return pal.TextDim
	case backend.StateCompleted:
		return pal.TextDim
	case backend.StateHighlight:
		return pal.Accent
	default:
		return pal.Text
	}
}

var (
	fontSans = font.Font{Typeface: "Go"}
	fontMono = font.Font{Typeface: "Go Mono"}
)

func newTheme() *material.Theme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts())
	th.Palette = material.Palette{
		Bg:         pal.Bg,
		Fg:         pal.Text,
		ContrastBg: pal.Accent,
		ContrastFg: pal.TextBright,
	}
	th.TextSize = 13
	th.Face = "Go"
	return th
}

func fill(gtx C, c color.NRGBA) D {
	sz := gtx.Constraints.Min
	paint.FillShape(gtx.Ops, c, clip.Rect{Max: sz}.Op())
	return D{Size: sz}
}

func fillRect(gtx C, c color.NRGBA, r image.Rectangle) {
	paint.FillShape(gtx.Ops, c, clip.Rect(r).Op())
}

// bg paints c behind w, sized to w.
func bg(gtx C, c color.NRGBA, w layout.Widget) D {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	call := m.Stop()
	fillRect(gtx, c, image.Rectangle{Max: d.Size})
	call.Add(gtx.Ops)
	return d
}

func hline(gtx C, c color.NRGBA) D {
	h := gtx.Dp(1)
	fillRect(gtx, c, image.Rect(0, 0, gtx.Constraints.Max.X, h))
	return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
}

func vline(gtx C, c color.NRGBA) D {
	w := gtx.Dp(1)
	fillRect(gtx, c, image.Rect(0, 0, w, gtx.Constraints.Max.Y))
	return D{Size: image.Pt(w, gtx.Constraints.Max.Y)}
}

var textOps = map[color.NRGBA]op.CallOp{}

// colorMat returns a cached paint material for text.
func colorMat(gtx C, c color.NRGBA) op.CallOp {
	m := op.Record(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	return m.Stop()
}

// lbl draws a single-line truncated label.
func lbl(gtx C, th *material.Theme, f font.Font, size unit.Sp, c color.NRGBA, s string) D {
	gtx.Constraints.Min.Y = 0 // let W/E directions center the line vertically
	l := widget.Label{MaxLines: 1, Truncator: "…", LineHeightScale: 1.05}
	return l.Layout(gtx, th.Shaper, f, size, s, colorMat(gtx, c))
}

func lblAlign(gtx C, th *material.Theme, f font.Font, size unit.Sp, c color.NRGBA, s string, a text.Alignment) D {
	gtx.Constraints.Min.Y = 0
	l := widget.Label{MaxLines: 1, Truncator: "…", Alignment: a, LineHeightScale: 1.05}
	return l.Layout(gtx, th.Shaper, f, size, s, colorMat(gtx, c))
}

func inset(v unit.Dp) layout.Inset { return layout.UniformInset(v) }

func insetHV(h, v unit.Dp) layout.Inset {
	return layout.Inset{Left: h, Right: h, Top: v, Bottom: v}
}

// button draws a flat Lens-like button.
func button(gtx C, th *material.Theme, cl *widget.Clickable, txt string, c color.NRGBA) D {
	return cl.Layout(gtx, func(gtx C) D {
		bgc := c
		if cl.Hovered() {
			bgc = lighten(c, 0.12)
		}
		return rbg(gtx, bgc, 6, func(gtx C) D {
			return insetHV(12, 6).Layout(gtx, func(gtx C) D {
				return lbl(gtx, th, fontSans, 13, pal.TextBright, txt)
			})
		})
	})
}

// flatButton is a text-only button.
func flatButton(gtx C, th *material.Theme, cl *widget.Clickable, txt string, active bool) D {
	return cl.Layout(gtx, func(gtx C) D {
		c := pal.TextDim
		if active || cl.Hovered() {
			c = pal.TextBright
		}
		return insetHV(8, 4).Layout(gtx, func(gtx C) D {
			return lbl(gtx, th, fontSans, 13, c, txt)
		})
	})
}

func lighten(c color.NRGBA, f float32) color.NRGBA {
	l := func(v uint8) uint8 { return uint8(float32(v) + (255-float32(v))*f) }
	return color.NRGBA{R: l(c.R), G: l(c.G), B: l(c.B), A: c.A}
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

func rect(x0, y0, x1, y1 int) image.Rectangle { return image.Rect(x0, y0, x1, y1) }

type colorNRGBA = color.NRGBA
