package ui

import (
	"fmt"
	"image"
	"strings"
	"unicode/utf8"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/giovanni/kubeon/internal/backend"
)

// detailsView renders backend.Details: sections with key/value grids, label
// chips, small tables and container cards. Links open the target resource.
type detailsView struct {
	app   *App
	links map[string]*widget.Clickable
	list  widget.List
}

func newDetailsView(a *App) *detailsView {
	d := &detailsView{app: a, links: map[string]*widget.Clickable{}}
	d.list.Axis = layout.Vertical
	return d
}

func (d *detailsView) reset() { d.links = map[string]*widget.Clickable{} }

func toneColor(t backend.Tone, def colorNRGBA) colorNRGBA {
	switch t {
	case backend.ToneOK:
		return pal.Ok
	case backend.ToneWarn:
		return pal.Warn
	case backend.ToneErr:
		return pal.Err
	case backend.ToneDim:
		return pal.TextDim
	}
	return def
}

// followLink navigates to a linked resource and opens its details.
func (a *App) followLink(l *backend.Link) {
	if l == nil || l.GVR == "" {
		return
	}
	if backend.IsNamespaced(l.GVR) {
		if i := strings.IndexByte(l.Path, '/'); i > 0 {
			a.ns = l.Path[:i]
		}
	}
	a.drill(navState{page: l.GVR, gvr: l.GVR, selID: l.Path})
	a.drawer.show(l.GVR, l.Path, tabOverview, false)
}

func (d *detailsView) field(gtx C, key string, f backend.Field) D {
	th := d.app.th
	c := toneColor(f.Tone, pal.TextBright)
	fnt := fontSans
	if f.Mono {
		fnt = fontMono
	}
	if f.Link != nil {
		cl := d.links[key]
		if cl == nil {
			cl = new(widget.Clickable)
			d.links[key] = cl
		}
		if cl.Clicked(gtx) {
			d.app.followLink(f.Link)
		}
		return cl.Layout(gtx, func(gtx C) D {
			col := pal.Accent
			if cl.Hovered() {
				col = lighten(pal.Accent, 0.25)
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return mdi(gtx, iconFor(f.Link.GVR), col, 13) }),
				layout.Rigid(layout.Spacer{Width: 5}.Layout),
				layout.Flexed(1, func(gtx C) D { return d.app.tipLbl(gtx, "det/link/"+key, fnt, 12.5, col, f.Value) }),
			)
		})
	}
	if f.Value == "" {
		return lbl(gtx, th, fnt, 12.5, pal.TextDim, "—")
	}
	l := material.Body2(th, f.Value)
	l.Font, l.TextSize, l.Color, l.MaxLines = fnt, 12.5, c, 4
	return l.Layout(gtx)
}

func (d *detailsView) fields(gtx C, key string, ff []backend.Field) D {
	th := d.app.th
	var ch []layout.FlexChild
	for i, f := range ff {
		i, f := i, f
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 5}.Layout(gtx, func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						w := gtx.Dp(130)
						gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
						return lbl(gtx, th, fontSans, 12, pal.TextDim, f.Key)
					}),
					layout.Flexed(1, func(gtx C) D { return d.field(gtx, fmt.Sprintf("%s/f%d", key, i), f) }),
				)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
}

// flow lays children out left-to-right, wrapping lines.
func flow(gtx C, gap int, children []layout.Widget) D {
	maxW := gtx.Constraints.Max.X
	x, y, lineH, width := 0, 0, 0, 0
	for _, w := range children {
		m := op.Record(gtx.Ops)
		cg := gtx
		cg.Constraints.Min = image.Point{}
		dims := w(cg)
		call := m.Stop()
		if x > 0 && x+dims.Size.X > maxW {
			x, y = 0, y+lineH+gap
			lineH = 0
		}
		st := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		st.Pop()
		x += dims.Size.X + gap
		lineH = max(lineH, dims.Size.Y)
		width = max(width, x)
	}
	return D{Size: image.Pt(min(width, maxW), y+lineH)}
}

func (d *detailsView) chips(gtx C, cc []backend.Field) D {
	th := d.app.th
	var ws []layout.Widget
	for _, c := range cc {
		c := c
		ws = append(ws, func(gtx C) D {
			return rbg(gtx, pal.Hover, 5, func(gtx C) D {
				return insetHV(7, 2).Layout(gtx, func(gtx C) D {
					txt := c.Key
					if c.Value != "" {
						txt += "=" + c.Value
					}
					return lbl(gtx, th, fontMono, 11, pal.Text, txt)
				})
			})
		})
	}
	return flow(gtx, gtx.Dp(5), ws)
}

func (d *detailsView) table(gtx C, key string, t *backend.DTable) D {
	th := d.app.th
	// column weights from content length
	weights := make([]float32, len(t.Columns))
	for i, c := range t.Columns {
		weights[i] = float32(utf8.RuneCountInString(c) + 2)
	}
	for _, r := range t.Rows {
		for i, f := range r {
			if i < len(weights) {
				weights[i] = max(weights[i], float32(min(utf8.RuneCountInString(f.Value)+2, 48)))
			}
		}
	}
	row := func(gtx C, cells func(i int) layout.Widget, header bool) D {
		var ch []layout.FlexChild
		for i := range t.Columns {
			i := i
			ch = append(ch, layout.Flexed(weights[i], func(gtx C) D {
				return layout.Inset{Right: 8}.Layout(gtx, cells(i))
			}))
		}
		return layout.Inset{Top: 3, Bottom: 3}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, ch...)
		})
	}
	var ch []layout.FlexChild
	ch = append(ch, layout.Rigid(func(gtx C) D {
		return row(gtx, func(i int) layout.Widget {
			return func(gtx C) D { return lbl(gtx, th, fontSans, 10.5, pal.TextDim, t.Columns[i]) }
		}, true)
	}))
	ch = append(ch, layout.Rigid(func(gtx C) D { return hline(gtx, pal.Border) }))
	for ri, r := range t.Rows {
		ri, r := ri, r
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return row(gtx, func(i int) layout.Widget {
				return func(gtx C) D {
					if i >= len(r) {
						return D{}
					}
					f := r[i]
					if f.Link != nil {
						return d.field(gtx, fmt.Sprintf("%s/t%d/%d", key, ri, i), f)
					}
					fnt := fontSans
					if f.Mono {
						fnt = fontMono
					}
					return d.app.tipLbl(gtx, fmt.Sprintf("det/%s/t%d/%d", key, ri, i), fnt, 12, toneColor(f.Tone, pal.Text), f.Value)
				}
			}, false)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
}

func (d *detailsView) section(gtx C, key string, s *backend.Section, card bool) D {
	th := d.app.th
	var ch []layout.FlexChild
	ch = append(ch, layout.Rigid(func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				if card {
					return iconText(gtx, th, icTerminal, 13, pal.TextBright, s.Title)
				}
				return lbl(gtx, th, fontSans, 11, pal.TextDim, upper(s.Title))
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Flexed(1, func(gtx C) D {
				if s.Badge.Value == "" {
					return D{}
				}
				c := toneColor(s.Badge.Tone, pal.Text)
				return rbg(gtx, withAlpha(c, 0x26), 9, func(gtx C) D {
					return insetHV(7, 1).Layout(gtx, func(gtx C) D { return lbl(gtx, th, fontSans, 11, c, s.Badge.Value) })
				})
			}),
		)
	}))
	gap := func() layout.FlexChild { return layout.Rigid(layout.Spacer{Height: 8}.Layout) }
	if len(s.Fields) > 0 {
		ch = append(ch, gap(), layout.Rigid(func(gtx C) D { return d.fields(gtx, key, s.Fields) }))
	}
	if len(s.Chips) > 0 {
		ch = append(ch, gap(), layout.Rigid(func(gtx C) D { return d.chips(gtx, s.Chips) }))
	}
	if s.Table != nil && len(s.Table.Rows) > 0 {
		ch = append(ch, gap(), layout.Rigid(func(gtx C) D { return d.table(gtx, key, s.Table) }))
	}
	if s.Text != "" {
		ch = append(ch, gap(), layout.Rigid(func(gtx C) D {
			l := material.Body2(th, s.Text)
			l.Font, l.TextSize, l.Color = fontMono, 11.5, pal.Text
			return l.Layout(gtx)
		}))
	}
	for i := range s.Cards {
		i := i
		ch = append(ch, gap(), layout.Rigid(func(gtx C) D {
			return rborder(gtx, pal.Border, pal.Bg, 8, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return inset(10).Layout(gtx, func(gtx C) D {
					return d.section(gtx, fmt.Sprintf("%s/c%d", key, i), &s.Cards[i], true)
				})
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
}

// Layout draws all sections.
func (d *detailsView) Layout(gtx C, det *backend.Details) D {
	th := d.app.th
	if det == nil {
		return layout.Center.Layout(gtx, func(gtx C) D {
			return lbl(gtx, th, fontSans, 13, pal.TextDim, "No details available")
		})
	}
	return material.List(th, &d.list).Layout(gtx, len(det.Sections), func(gtx C, i int) D {
		return layout.Inset{Left: unit.Dp(12), Right: 12, Top: 10, Bottom: 6}.Layout(gtx, func(gtx C) D {
			return d.section(gtx, fmt.Sprintf("s%d", i), &det.Sections[i], false)
		})
	})
}
