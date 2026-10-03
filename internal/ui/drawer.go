package ui

import (
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const (
	tabDescribe = iota
	tabYAML
)

// Drawer is the right-hand details panel (describe / YAML with editing).
type Drawer struct {
	app       *App
	open      bool
	gvr, path string
	tab       int
	editing   bool
	dirty     bool
	original  string
	loading   bool
	errMsg    string
	seq       int

	describe widget.Editor
	yaml     widget.Editor

	tabBtns                               [2]widget.Clickable
	closeBtn, editBtn, saveBtn, revertBtn widget.Clickable
}

func newDrawer(a *App) *Drawer {
	d := Drawer{app: a}
	d.describe.ReadOnly = true
	d.yaml.ReadOnly = true
	return &d
}

func (d *Drawer) focused(gtx C) bool {
	return d.open && (gtx.Focused(&d.yaml) && d.editing)
}

func (d *Drawer) show(gvr, path string, tab int, edit bool) {
	d.open, d.tab = true, tab
	if d.gvr != gvr || d.path != path {
		d.gvr, d.path = gvr, path
		d.load()
	}
	d.setEditing(edit)
}

// follow tracks table selection while not editing.
func (d *Drawer) follow(gvr, path string) {
	if d.editing && d.dirty {
		return
	}
	d.setEditing(false)
	if d.gvr != gvr || d.path != path {
		d.gvr, d.path = gvr, path
		d.load()
	}
}

func (d *Drawer) close() {
	d.open, d.editing, d.dirty = false, false, false
	d.gvr, d.path = "", ""
}

func (d *Drawer) setEditing(on bool) {
	d.editing = on
	d.yaml.ReadOnly = !on
	if on {
		d.tab = tabYAML
	}
}

func (d *Drawer) load() {
	c := d.app.cluster
	if c == nil {
		return
	}
	d.seq++
	seq, gvr, path := d.seq, d.gvr, d.path
	d.loading, d.errMsg, d.dirty = true, "", false
	go func() {
		desc, derr := c.Describe(gvr, path)
		y, yerr := c.YAML(gvr, path)
		d.app.post(func() {
			if seq != d.seq {
				return
			}
			d.loading = false
			if derr != nil {
				desc = derr.Error()
			}
			if yerr != nil {
				d.errMsg = yerr.Error()
			}
			d.describe.SetText(desc)
			d.yaml.SetText(y)
			d.original = y
		})
	}()
}

func (d *Drawer) save() {
	c := d.app.cluster
	gvr, manifest := d.gvr, d.yaml.Text()
	d.app.do("Apply "+d.path, func() error {
		if err := c.Apply(gvr, manifest); err != nil {
			return err
		}
		d.app.post(func() {
			d.setEditing(false)
			d.load()
		})
		return nil
	})
}

// Layout draws the drawer.
func (d *Drawer) Layout(gtx C) D {
	th := d.app.th
	for i := range d.tabBtns {
		if d.tabBtns[i].Clicked(gtx) {
			d.tab = i
		}
	}
	if d.closeBtn.Clicked(gtx) {
		d.close()
		return D{}
	}
	if d.editBtn.Clicked(gtx) {
		d.setEditing(true)
	}
	if d.revertBtn.Clicked(gtx) {
		d.yaml.SetText(d.original)
		d.dirty = false
		d.setEditing(false)
	}
	if d.saveBtn.Clicked(gtx) {
		d.save()
	}
	for {
		ev, ok := d.yaml.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok && d.editing {
			d.dirty = d.yaml.Text() != d.original
		}
	}
	fill(gtx, pal.Panel)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return bg(gtx, pal.Header, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return insetHV(12, 8).Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							return lbl(gtx, th, fontSans, 14, pal.TextBright, titleFor(d.gvr)+": "+d.path)
						}),
						layout.Rigid(func(gtx C) D { return iconButton(gtx, &d.closeBtn, iconClose) }),
					)
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			return insetHV(8, 4).Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return d.tabButton(gtx, 0, "Describe") }),
					layout.Rigid(func(gtx C) D { return d.tabButton(gtx, 1, "YAML") }),
					layout.Flexed(1, layout.Spacer{}.Layout),
					layout.Rigid(func(gtx C) D {
						if d.tab != tabYAML {
							return D{}
						}
						if !d.editing {
							return flatButton(gtx, th, &d.editBtn, "Edit", false)
						}
						return layout.Flex{}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return flatButton(gtx, th, &d.revertBtn, "Cancel", false) }),
							layout.Rigid(layout.Spacer{Width: 6}.Layout),
							layout.Rigid(func(gtx C) D {
								c := pal.Accent
								if !d.dirty {
									c = pal.Border
								}
								return button(gtx, th, &d.saveBtn, "Save & Apply", c)
							}),
						)
					}),
				)
			})
		}),
		layout.Rigid(func(gtx C) D { return hline(gtx, pal.Border) }),
		layout.Flexed(1, func(gtx C) D {
			if d.loading {
				return layout.Center.Layout(gtx, func(gtx C) D {
					return lbl(gtx, th, fontSans, 13, pal.TextDim, "Loading…")
				})
			}
			gtx.Constraints.Min = gtx.Constraints.Max
			if d.editing {
				fill(gtx, pal.Bg)
			}
			return inset(10).Layout(gtx, func(gtx C) D {
				ed := &d.describe
				if d.tab == tabYAML {
					ed = &d.yaml
				}
				e := material.Editor(th, ed, "")
				e.Font = fontMono
				e.TextSize = 12
				e.Color = pal.Text
				if d.tab == tabYAML && d.errMsg != "" {
					e.Hint = d.errMsg
				}
				return e.Layout(gtx)
			})
		}),
	)
}

func (d *Drawer) tabButton(gtx C, i int, txt string) D {
	th := d.app.th
	active := d.tab == i
	dims := flatButton(gtx, th, &d.tabBtns[i], txt, active)
	if active {
		fillRect(gtx, pal.Accent, rect(0, dims.Size.Y-gtx.Dp(2), dims.Size.X, dims.Size.Y))
	}
	return dims
}
