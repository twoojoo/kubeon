package ui

import (
	"image"

	"gioui.org/f32"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const titleBarHeight = unit.Dp(40)

// TitleBar replaces the platform/Gio decorations (app.Decorated(false)).
// Empty regions are move areas: drag moves the window (also when
// maximized), double-click toggles maximize, right-click opens the window
// menu (see third_party/_gio-patches).
type TitleBar struct {
	app                      *App
	minBtn, maxBtn, closeBtn widget.Clickable
	ctxBtn, nsBtn, searchBtn widget.Clickable
}

func newTitleBar(a *App) *TitleBar { return &TitleBar{app: a} }

// moveArea marks the current constraints as a window drag handle.
func moveArea(gtx C) D {
	sz := gtx.Constraints.Max
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
	return D{Size: sz}
}

// moveBehind lays out w and makes its whole box a drag handle beneath it.
func moveBehind(gtx C, w layout.Widget) D {
	d := w(gtx)
	st := clip.Rect{Max: d.Size}.Push(gtx.Ops)
	system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
	st.Pop()
	return d
}

func (t *TitleBar) perform(act system.Action) {
	if t.app.w != nil {
		t.app.w.Perform(act)
	}
}

func (t *TitleBar) Layout(gtx C) D {
	a := t.app
	th := a.th
	if t.minBtn.Clicked(gtx) {
		t.perform(system.ActionMinimize)
	}
	if t.maxBtn.Clicked(gtx) {
		if a.maximized {
			t.perform(system.ActionUnmaximize)
		} else {
			t.perform(system.ActionMaximize)
		}
	}
	if t.closeBtn.Clicked(gtx) {
		t.perform(system.ActionClose)
	}
	if t.ctxBtn.Clicked(gtx) {
		a.pickContext()
	}
	if t.nsBtn.Clicked(gtx) {
		a.pickNamespace()
	}
	if t.searchBtn.Clicked(gtx) {
		a.openPalette("")
	}

	h := gtx.Dp(titleBarHeight)
	w := gtx.Constraints.Max.X
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	dim := !a.winFocused && a.w != nil
	fg, fgDim := pal.TextBright, pal.Text
	if dim {
		fg, fgDim = pal.TextDim, pal.TextDim
	}
	// same surface as the sidebar/panels; the brand block lines up with
	// the sidebar via a divider
	fillRect(gtx, pal.Sidebar, image.Rect(0, 0, w, h))
	sideW := gtx.Dp(240)
	fillRect(gtx, pal.Border, image.Rect(sideW, gtx.Dp(8), sideW+gtx.Dp(1), h-gtx.Dp(8)))
	fillRect(gtx, pal.Border, image.Rect(0, h-gtx.Dp(1), w, h))

	center := func(wd layout.Widget) layout.Widget {
		return func(gtx C) D {
			gtx.Constraints.Min.Y = h
			return layout.W.Layout(gtx, wd)
		}
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		// brand (aligned with the sidebar)
		layout.Rigid(func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(sideW, h))
			return moveBehind(gtx, func(gtx C) D {
				gtx.Constraints.Min = gtx.Constraints.Max
				return layout.Inset{Left: 14}.Layout(gtx, center(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return t.logo(gtx) }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 14, fg, "k9desk") }),
					)
				}))
			})
		}),
		// breadcrumb: cluster › namespace › page
		layout.Rigid(center(func(gtx C) D {
			if a.cluster == nil {
				return D{}
			}
			return layout.Inset{Left: 10}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return crumb(gtx, &t.ctxBtn, func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D { return avatar(gtx, th, a.cluster.Context, 18) }),
								layout.Rigid(layout.Spacer{Width: 7}.Layout),
								layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 13, fg, a.cluster.Context) }),
								layout.Rigid(func(gtx C) D { return mdi(gtx, icExpand, pal.TextDim, 16) }),
							)
						})
					}),
					layout.Rigid(func(gtx C) D { return mdi(gtx, icCollapse, pal.Border, 16) }),
					layout.Rigid(func(gtx C) D {
						return crumb(gtx, &t.nsBtn, func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D { return mdi(gtx, icNamespace, pal.TextDim, 14) }),
								layout.Rigid(layout.Spacer{Width: 6}.Layout),
								layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 13, fgDim, nsLabel(a.ns)) }),
								layout.Rigid(func(gtx C) D { return mdi(gtx, icExpand, pal.TextDim, 16) }),
							)
						})
					}),
					layout.Rigid(func(gtx C) D { return mdi(gtx, icCollapse, pal.Border, 16) }),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D {
							return lbl(gtx, th, fontSans, 13, fgDim, a.pageTitle())
						})
					}),
				)
			})
		})),
		layout.Flexed(1, moveArea),
		// command/search box
		layout.Rigid(center(func(gtx C) D {
			if a.cluster == nil {
				return D{}
			}
			bw := min(gtx.Dp(380), max(gtx.Dp(160), w/4))
			gtx.Constraints = layout.Exact(image.Pt(bw, gtx.Dp(28)))
			return t.searchBtn.Layout(gtx, func(gtx C) D {
				b := pal.Border
				if t.searchBtn.Hovered() {
					b = pal.TextDim
				}
				return rborder(gtx, b, pal.Bg, 7, func(gtx C) D {
					gtx.Constraints.Min = gtx.Constraints.Max
					return insetHV(10, 0).Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return mdi(gtx, icSearch, pal.TextDim, 15) }),
							layout.Rigid(layout.Spacer{Width: 8}.Layout),
							layout.Flexed(1, func(gtx C) D {
								gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
								return layout.W.Layout(gtx, func(gtx C) D {
									return lbl(gtx, th, fontSans, 12, pal.TextDim, "Search resources, run commands…")
								})
							}),
							layout.Rigid(func(gtx C) D { return keyChip(gtx, th, "Ctrl K") }),
						)
					})
				})
			})
		})),
		layout.Flexed(1, moveArea),
		// window controls
		layout.Rigid(func(gtx C) D { return t.winButton(gtx, &t.minBtn, glyphMinimize, false) }),
		layout.Rigid(func(gtx C) D {
			g := glyphMaximize
			if a.maximized {
				g = glyphRestore
			}
			return t.winButton(gtx, &t.maxBtn, g, false)
		}),
		layout.Rigid(func(gtx C) D { return t.winButton(gtx, &t.closeBtn, glyphClose, true) }),
	)
}

func crumb(gtx C, cl *widget.Clickable, w layout.Widget) D {
	return cl.Layout(gtx, func(gtx C) D {
		b := colorNRGBA{}
		if cl.Hovered() {
			b = pal.Hover
		}
		return rbg(gtx, b, 6, func(gtx C) D {
			return insetHV(7, 4).Layout(gtx, w)
		})
	})
}

func keyChip(gtx C, th *material.Theme, k string) D {
	return rborder(gtx, pal.Border, pal.Panel, 4, func(gtx C) D {
		return insetHV(5, 1).Layout(gtx, func(gtx C) D {
			return lbl(gtx, th, fontSans, 10, pal.TextDim, k)
		})
	})
}

// logo draws the k9desk mark: an accent tile with a stylized helm wheel.
func (t *TitleBar) logo(gtx C) D {
	s := gtx.Dp(24)
	fillRRect(gtx, pal.Accent, image.Rect(0, 0, s, s), gtx.Dp(6))
	c := f32.Pt(float32(s)/2, float32(s)/2)
	r := float32(s) * 0.28
	var p clip.Path
	p.Begin(gtx.Ops)
	// wheel rim
	const k = 0.5523 // circle bezier factor
	p.MoveTo(f32.Pt(c.X+r, c.Y))
	p.CubeTo(f32.Pt(c.X+r, c.Y+r*k), f32.Pt(c.X+r*k, c.Y+r), f32.Pt(c.X, c.Y+r))
	p.CubeTo(f32.Pt(c.X-r*k, c.Y+r), f32.Pt(c.X-r, c.Y+r*k), f32.Pt(c.X-r, c.Y))
	p.CubeTo(f32.Pt(c.X-r, c.Y-r*k), f32.Pt(c.X-r*k, c.Y-r), f32.Pt(c.X, c.Y-r))
	p.CubeTo(f32.Pt(c.X+r*k, c.Y-r), f32.Pt(c.X+r, c.Y-r*k), f32.Pt(c.X+r, c.Y))
	// spokes
	R := float32(s) * 0.40
	for _, d := range []f32.Point{{X: 1, Y: 0}, {X: 0, Y: 1}, {X: 0.7071, Y: 0.7071}, {X: 0.7071, Y: -0.7071}} {
		p.MoveTo(f32.Pt(c.X-d.X*R, c.Y-d.Y*R))
		p.LineTo(f32.Pt(c.X+d.X*R, c.Y+d.Y*R))
	}
	paint.FillShape(gtx.Ops, pal.TextBright, clip.Stroke{Path: p.End(), Width: float32(gtx.Dp(1.8))}.Op())
	return D{Size: image.Pt(s, s)}
}

type winGlyph int

const (
	glyphMinimize winGlyph = iota
	glyphMaximize
	glyphRestore
	glyphClose
)

func (t *TitleBar) winButton(gtx C, cl *widget.Clickable, g winGlyph, danger bool) D {
	w, h := gtx.Dp(46), gtx.Dp(titleBarHeight)
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	return cl.Layout(gtx, func(gtx C) D {
		c := pal.Text
		if !t.app.winFocused && t.app.w != nil {
			c = pal.TextDim
		}
		if cl.Hovered() {
			if danger {
				fillRect(gtx, rgb(0xd83b3b), image.Rect(0, 0, w, h-gtx.Dp(1)))
				c = pal.TextBright
			} else {
				fillRect(gtx, pal.Hover, image.Rect(0, 0, w, h-gtx.Dp(1)))
				c = pal.TextBright
			}
		}
		s := float32(gtx.Dp(10))
		ox, oy := (float32(w)-s)/2, (float32(h)-s)/2
		sw := float32(gtx.Dp(1.2))
		var p clip.Path
		p.Begin(gtx.Ops)
		rectPath := func(x, y, rw, rh float32) {
			p.MoveTo(f32.Pt(x, y))
			p.LineTo(f32.Pt(x+rw, y))
			p.LineTo(f32.Pt(x+rw, y+rh))
			p.LineTo(f32.Pt(x, y+rh))
			p.Close()
		}
		switch g {
		case glyphMinimize:
			p.MoveTo(f32.Pt(ox, oy+s/2+sw))
			p.LineTo(f32.Pt(ox+s, oy+s/2+sw))
		case glyphMaximize:
			rectPath(ox, oy, s, s)
		case glyphRestore:
			o := s * 0.25
			rectPath(ox, oy+o, s-o, s-o)
			p.MoveTo(f32.Pt(ox+o, oy+o))
			p.LineTo(f32.Pt(ox+o, oy))
			p.LineTo(f32.Pt(ox+s, oy))
			p.LineTo(f32.Pt(ox+s, oy+s-o))
			p.LineTo(f32.Pt(ox+s-o, oy+s-o))
		case glyphClose:
			p.MoveTo(f32.Pt(ox, oy))
			p.LineTo(f32.Pt(ox+s, oy+s))
			p.MoveTo(f32.Pt(ox+s, oy))
			p.LineTo(f32.Pt(ox, oy+s))
		}
		paint.FillShape(gtx.Ops, c, clip.Stroke{Path: p.End(), Width: sw}.Op())
		return D{Size: image.Pt(w, h)}
	})
}

// pageTitle names the current main page for the breadcrumb.
func (a *App) pageTitle() string {
	switch a.page {
	case overviewGVR:
		return "Overview"
	case xrayPage:
		return "XRay · " + titleFor(a.xray.gvr)
	case "":
		return ""
	}
	return a.view.title
}
