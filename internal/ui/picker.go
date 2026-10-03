package ui

import (
	"image"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Picker is a filterable list overlay (namespaces, contexts, command palette).
type Picker struct {
	app       *App
	title     string
	items     []string
	matches   []int
	sel       int
	allowFree bool
	onPick    func(string)
	exact     func(string) bool // free text accepted as-is when true
	done      bool
	focused   bool
	lastQ     string

	editor widget.Editor
	list   widget.List
	tag    int
}

func newPicker(a *App, title string, items []string, allowFree bool, onPick func(string)) *Picker {
	p := Picker{app: a, title: title, items: items, allowFree: allowFree, onPick: onPick, lastQ: "\x00"}
	p.editor.SingleLine = true
	p.editor.Submit = true
	p.list.Axis = layout.Vertical
	return &p
}

func (p *Picker) Done() bool { return p.done }

func (p *Picker) refilter() {
	q := strings.ToLower(strings.TrimSpace(p.editor.Text()))
	if q == p.lastQ {
		return
	}
	p.lastQ = q
	p.matches = p.matches[:0]
	terms := strings.Fields(q)
	var prefix, rest []int
	for i, it := range p.items {
		l := strings.ToLower(it)
		ok := true
		for _, t := range terms {
			if !strings.Contains(l, t) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if len(terms) > 0 && strings.HasPrefix(l, terms[0]) {
			prefix = append(prefix, i)
		} else {
			rest = append(rest, i)
		}
	}
	p.matches = append(append(p.matches, prefix...), rest...)
	p.sel = 0
	p.list.Position = layout.Position{}
}

func (p *Picker) pick(gtx C) {
	raw := strings.TrimSpace(p.editor.Text())
	var val string
	switch {
	case p.allowFree && raw != "" && p.exact != nil && p.exact(raw):
		val = raw
	case p.sel >= 0 && p.sel < len(p.matches):
		val = p.items[p.matches[p.sel]]
	case p.allowFree:
		val = raw
	default:
		return
	}
	p.done = true
	gtx.Execute(key.FocusCmd{})
	if val != "" {
		p.onPick(val)
	}
}

func (p *Picker) Layout(gtx C) D {
	th := p.app.th
	if !p.focused {
		gtx.Execute(key.FocusCmd{Tag: &p.editor})
		p.focused = true
	}
	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: &p.editor, Name: key.NameUpArrow},
			key.Filter{Focus: &p.editor, Name: key.NameDownArrow},
			key.Filter{Focus: &p.editor, Name: key.NameTab},
		)
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			switch e.Name {
			case key.NameUpArrow:
				p.sel = max(0, p.sel-1)
			case key.NameDownArrow, key.NameTab:
				p.sel = min(len(p.matches)-1, p.sel+1)
			}
			p.list.ScrollTo(max(0, p.sel-5))
		}
	}
	for {
		ev, ok := p.editor.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			p.pick(gtx)
		}
	}
	p.refilter()

	w := min(gtx.Dp(640), gtx.Constraints.Max.X-gtx.Dp(40))
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
	return bg(gtx, pal.Border, func(gtx C) D {
		return inset(1).Layout(gtx, func(gtx C) D {
			return bg(gtx, pal.Panel, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 14, Right: 14, Top: 10}.Layout(gtx, func(gtx C) D {
							return lbl(gtx, th, fontSans, 12, pal.TextDim, p.title)
						})
					}),
					layout.Rigid(func(gtx C) D {
						return insetHV(14, 8).Layout(gtx, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							e := material.Editor(th, &p.editor, "Type to filter…")
							e.TextSize = 15
							e.Color = pal.TextBright
							e.HintColor = pal.TextDim
							return e.Layout(gtx)
						})
					}),
					layout.Rigid(func(gtx C) D { return hline(gtx, pal.Border) }),
					layout.Rigid(func(gtx C) D {
						if len(p.items) == 0 {
							return D{}
						}
						rh := gtx.Dp(28)
						n := min(len(p.matches), 14)
						gtx.Constraints.Max.Y = max(rh, n*rh)
						gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
						area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
						event.Op(gtx.Ops, &p.tag)
						for {
							ev, ok := gtx.Event(pointer.Filter{Target: &p.tag, Kinds: pointer.Press})
							if !ok {
								break
							}
							if pe, ok := ev.(pointer.Event); ok {
								idx := p.list.Position.First + (int(pe.Position.Y)+p.list.Position.Offset)/rh
								if idx < len(p.matches) {
									p.sel = idx
									p.pick(gtx)
								}
							}
						}
						d := material.List(th, &p.list).Layout(gtx, len(p.matches), func(gtx C, i int) D {
							sz := image.Pt(gtx.Constraints.Max.X, rh)
							if i == p.sel {
								fillRect(gtx, pal.Selected, image.Rectangle{Max: sz})
							}
							gtx.Constraints = layout.Exact(sz)
							return layout.Inset{Left: unit.Dp(14)}.Layout(gtx, func(gtx C) D {
								return layout.W.Layout(gtx, func(gtx C) D {
									c := pal.Text
									if i == p.sel {
										c = pal.TextBright
									}
									return lbl(gtx, th, fontSans, 13, c, p.items[p.matches[i]])
								})
							})
						})
						area.Pop()
						return d
					}),
				)
			})
		})
	})
}

// Confirm is a yes/no modal; danger dialogs offer a "force" option.
type Confirm struct {
	app            *App
	title, msg     string
	danger, force  bool
	onOK           func(force bool)
	done           bool
	ok, cancel, fb widget.Clickable
}

func newConfirm(a *App, title, msg string, danger bool, onOK func(bool)) *Confirm {
	return &Confirm{app: a, title: title, msg: msg, danger: danger, onOK: onOK}
}

func (c *Confirm) Done() bool { return c.done }

func (c *Confirm) Layout(gtx C) D {
	th := c.app.th
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameReturn}, key.Filter{Name: key.NameEnter})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			c.done = true
			c.onOK(c.force)
		}
	}
	if c.ok.Clicked(gtx) {
		c.done = true
		c.onOK(c.force)
	}
	if c.cancel.Clicked(gtx) {
		c.done = true
	}
	if c.fb.Clicked(gtx) {
		c.force = !c.force
	}
	w := min(gtx.Dp(460), gtx.Constraints.Max.X-gtx.Dp(40))
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
	return bg(gtx, pal.Border, func(gtx C) D {
		return inset(1).Layout(gtx, func(gtx C) D {
			return bg(gtx, pal.Panel, func(gtx C) D {
				return inset(18).Layout(gtx, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 16, pal.TextBright, c.title) }),
						layout.Rigid(layout.Spacer{Height: 10}.Layout),
						layout.Rigid(func(gtx C) D {
							l := material.Body1(th, c.msg)
							l.Color = pal.Text
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: 16}.Layout),
						layout.Rigid(func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									if !c.danger {
										return D{}
									}
									return toggle(gtx, th, &c.fb, "Force (grace 0)", c.force)
								}),
								layout.Flexed(1, layout.Spacer{}.Layout),
								layout.Rigid(func(gtx C) D { return flatButton(gtx, th, &c.cancel, "Cancel", false) }),
								layout.Rigid(layout.Spacer{Width: 8}.Layout),
								layout.Rigid(func(gtx C) D {
									col := pal.Accent
									if c.danger {
										col = pal.Err
									}
									return button(gtx, th, &c.ok, c.title, col)
								}),
							)
						}),
					)
				})
			})
		})
	})
}
