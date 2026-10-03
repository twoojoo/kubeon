package ui

import (
	"fmt"
	"image"
	"strings"
	"time"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/giovanni/k9desk/internal/backend"
)

const (
	rowHeight    = unit.Dp(26)
	headerHeight = unit.Dp(30)
	cellPad      = unit.Dp(10)
	tableText    = unit.Sp(13)
)

// ResourceView is the main resource table: a virtualized list over the
// latest backend snapshot. Only visible rows are laid out each frame.
type ResourceView struct {
	app   *App
	gvr   string
	ns    string
	title string
	watch *backend.TableWatch
	snap  *backend.Snapshot

	list    widget.List
	sel     int
	selID   string
	widths  []int
	widthOf struct {
		seq   uint64
		avail int
	}
	headerClicks []widget.Clickable
	rowsTag      int
	lastClick    time.Time
	lastClickRow int

	filter     widget.Editor
	lastFilter string
	wide       bool

	pfBtn, nsBtn, logsBtn, shellBtn, yamlBtn, descBtn, delBtn, scaleBtn, restartBtn widget.Clickable
}

func newResourceView(a *App) *ResourceView {
	v := ResourceView{app: a, sel: -1}
	v.list.Axis = layout.Vertical
	v.filter.SingleLine = true
	v.filter.Submit = true
	return &v
}

func (v *ResourceView) open(gvr, ns string) {
	v.close()
	v.gvr, v.ns = gvr, ns
	v.title = titleFor(gvr)
	v.snap, v.sel, v.selID = nil, -1, ""
	v.widths = nil
	v.list.Position = layout.Position{}
	c := v.app.cluster
	tw := c.Watch(gvr, ns, func(s *backend.Snapshot) {
		v.app.post(func() { v.onSnapshot(s) })
	})
	v.watch = tw
	if f := v.filter.Text(); f != "" {
		tw.SetFilter(f)
	}
	tw.SetWide(v.wide)
}

func (v *ResourceView) close() {
	if v.watch != nil {
		v.watch.Close()
		v.watch = nil
	}
}

func (v *ResourceView) onSnapshot(s *backend.Snapshot) {
	if v.watch == nil || s.GVR != v.watch.GVR() {
		return
	}
	v.snap = s
	// keep selection on the same object across updates
	if v.selID != "" {
		v.sel = -1
		for i := range s.Rows {
			if s.Rows[i].ID == v.selID {
				v.sel = i
				break
			}
		}
	}
	if v.sel < 0 && len(s.Rows) > 0 {
		v.sel = 0
		v.selID = s.Rows[0].ID
	}
	if v.sel >= len(s.Rows) {
		v.sel = len(s.Rows) - 1
	}
}

func (v *ResourceView) selected() (backend.Row, bool) {
	if v.snap == nil || v.sel < 0 || v.sel >= len(v.snap.Rows) {
		return backend.Row{}, false
	}
	return v.snap.Rows[v.sel], true
}

func (v *ResourceView) setSel(i int) {
	if v.snap == nil || len(v.snap.Rows) == 0 {
		return
	}
	i = max(0, min(i, len(v.snap.Rows)-1))
	v.sel, v.selID = i, v.snap.Rows[i].ID
	v.ensureVisible()
	if v.app.drawer.open {
		v.app.drawer.follow(v.gvr, v.selID)
	}
}

func (v *ResourceView) move(d int) {
	if v.sel < 0 {
		v.setSel(0)
		return
	}
	v.setSel(v.sel + d)
}

func (v *ResourceView) pageSize() int {
	return max(1, v.list.Position.Count-1)
}

func (v *ResourceView) ensureVisible() {
	p := &v.list.Position
	if v.sel < p.First || (v.sel == p.First && p.Offset > 0) {
		p.First, p.Offset = v.sel, 0
		p.BeforeEnd = true
		return
	}
	if p.Count > 0 && v.sel >= p.First+p.Count-1 {
		p.First, p.Offset = v.sel-p.Count+2, 0
		p.BeforeEnd = true
	}
}

func (v *ResourceView) applyFilter() {
	q := strings.TrimSpace(v.filter.Text())
	if q == v.lastFilter {
		return
	}
	v.lastFilter = q
	if v.watch != nil {
		v.watch.SetFilter(q)
	}
}

func (v *ResourceView) toggleWide() {
	v.wide = !v.wide
	if v.watch != nil {
		v.watch.SetWide(v.wide)
	}
}

func (v *ResourceView) refresh() {
	if w := v.watch; w != nil {
		go w.Refresh()
	}
}

// Layout draws toolbar + header + rows.
func (v *ResourceView) Layout(gtx C) D {
	for {
		ev, ok := v.filter.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent:
			v.applyFilter()
		case widget.SubmitEvent:
			v.applyFilter()
			gtx.Execute(key.FocusCmd{})
		}
	}
	v.handleToolbar(gtx)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(v.layoutToolbar),
		layout.Rigid(func(gtx C) D { return hline(gtx, pal.Border) }),
		layout.Flexed(1, v.layoutTable),
	)
}

func (v *ResourceView) handleToolbar(gtx C) {
	if v.nsBtn.Clicked(gtx) {
		v.app.pickNamespace()
	}
	if v.logsBtn.Clicked(gtx) {
		v.logs(false)
	}
	if v.shellBtn.Clicked(gtx) {
		v.shell()
	}
	if v.yamlBtn.Clicked(gtx) {
		v.details(tabYAML)
	}
	if v.descBtn.Clicked(gtx) {
		v.details(tabDescribe)
	}
	if v.pfBtn.Clicked(gtx) {
		v.portForward()
	}
	if v.scaleBtn.Clicked(gtx) {
		v.scale()
	}
	if v.restartBtn.Clicked(gtx) {
		v.restart()
	}
	if v.delBtn.Clicked(gtx) {
		v.confirmDelete()
	}
}

func (v *ResourceView) layoutToolbar(gtx C) D {
	th := v.app.th
	roomy := gtx.Constraints.Max.X > gtx.Dp(1050)
	return bg(gtx, pal.Panel, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return insetHV(14, 8).Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return lbl(gtx, th, fontSans, 17, pal.TextBright, v.title)
				}),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Rigid(func(gtx C) D {
					s := v.snap
					if s == nil || s.Loading {
						return lbl(gtx, th, fontSans, 12, pal.TextDim, "loading…")
					}
					n := fmt.Sprintf("%d items", len(s.Rows))
					if len(s.Rows) != s.Total {
						n = fmt.Sprintf("%d / %d items", len(s.Rows), s.Total)
					}
					return lbl(gtx, th, fontSans, 12, pal.TextDim, n)
				}),
				layout.Flexed(1, layout.Spacer{}.Layout),
				layout.Rigid(func(gtx C) D {
					if !roomy {
						return D{}
					}
					return v.layoutActions(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: 12}.Layout),
				layout.Rigid(func(gtx C) D {
					if !backend.IsNamespaced(v.gvr) {
						return D{}
					}
					label := "Namespace: " + nsLabel(v.ns)
					return bg(gtx, pal.Bg, func(gtx C) D {
						return v.nsBtn.Layout(gtx, func(gtx C) D {
							return insetHV(8, 4).Layout(gtx, func(gtx C) D {
								return labelIcon(gtx, th, 13, pal.TextBright, label, iconChevronDown)
							})
						})
					})
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Dp(240)
					gtx.Constraints.Max.X = gtx.Dp(240)
					return v.layoutFilter(gtx)
				}),
			)
		})
	})
}

func (v *ResourceView) layoutActions(gtx C) D {
	row, ok := v.selected()
	if !ok {
		return D{}
	}
	_ = row
	th := v.app.th
	c := v.app.cluster
	var items []layout.FlexChild
	add := func(cl *widget.Clickable, txt string, show bool) {
		if !show {
			return
		}
		items = append(items, layout.Rigid(func(gtx C) D { return flatButton(gtx, th, cl, txt, false) }))
	}
	isPod := v.gvr == "v1/pods"
	add(&v.logsBtn, "Logs", c.IsLoggable(v.gvr))
	add(&v.shellBtn, "Shell", isPod)
	add(&v.pfBtn, "Forward", canForward(v.gvr))
	add(&v.descBtn, "Describe", true)
	add(&v.yamlBtn, "YAML", true)
	add(&v.scaleBtn, "Scale", c.CanScale(v.gvr))
	add(&v.restartBtn, "Restart", c.CanRestart(v.gvr))
	add(&v.delBtn, "Delete", backend.CanVerb(v.gvr, "delete"))
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, items...)
}

func (v *ResourceView) layoutFilter(gtx C) D {
	th := v.app.th
	border := pal.Border
	if gtx.Focused(&v.filter) {
		border = pal.Accent
	}
	return bg(gtx, border, func(gtx C) D {
		return inset(1).Layout(gtx, func(gtx C) D {
			return bg(gtx, pal.Bg, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return insetHV(8, 5).Layout(gtx, func(gtx C) D {
					e := material.Editor(th, &v.filter, "Filter (regex, -f fuzzy, -l sel)")
					e.TextSize = 13
					e.Color = pal.TextBright
					e.HintColor = pal.TextDim
					return e.Layout(gtx)
				})
			})
		})
	})
}

func (v *ResourceView) layoutTable(gtx C) D {
	s := v.snap
	th := v.app.th
	if s == nil || (s.Loading && len(s.Rows) == 0) {
		return layout.Center.Layout(gtx, func(gtx C) D {
			return lbl(gtx, th, fontSans, 14, pal.TextDim, "Loading…")
		})
	}
	if s.Err != nil && len(s.Columns) == 0 {
		return layout.Center.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Max.X = gtx.Dp(600)
			l := material.Body1(th, s.Err.Error())
			l.Color = pal.Err
			return l.Layout(gtx)
		})
	}
	v.computeWidths(gtx, s)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return v.layoutHeader(gtx, s) }),
		layout.Flexed(1, func(gtx C) D { return v.layoutRows(gtx, s) }),
	)
}

// computeWidths sizes columns from content (sampled), then fits them to the
// available width. Recomputed only when the snapshot or width changes.
func (v *ResourceView) computeWidths(gtx C, s *backend.Snapshot) {
	avail := gtx.Constraints.Max.X
	if v.widths != nil && v.widthOf.seq == s.Seq && v.widthOf.avail == avail && len(v.widths) == len(s.Columns) {
		return
	}
	v.widthOf.seq, v.widthOf.avail = s.Seq, avail
	charW := float32(gtx.Sp(tableText)) * 0.6
	pad := gtx.Dp(cellPad) * 2
	maxLen := make([]int, len(s.Columns))
	for i, c := range s.Columns {
		maxLen[i] = utf8.RuneCountInString(c.Name) + 2
	}
	step := 1
	if len(s.Rows) > 2000 {
		step = len(s.Rows) / 2000
	}
	for r := 0; r < len(s.Rows); r += step {
		for i, cell := range s.Rows[r].Cells {
			if i < len(maxLen) {
				if n := utf8.RuneCountInString(cell); n > maxLen[i] {
					maxLen[i] = n
				}
			}
		}
	}
	w := make([]int, len(s.Columns))
	total := 0
	minW, maxW := gtx.Dp(40), gtx.Dp(460)
	for i, n := range maxLen {
		w[i] = max(minW, min(maxW, int(float32(n)*charW)+pad))
		total += w[i]
	}
	if total > avail && total > 0 {
		// NAME keeps up to 40% of the width; other columns are water-filled
		// (capped at a common level) so short ones stay intact
		nameIdx := -1
		for i, c := range s.Columns {
			if c.Name == "NAME" {
				nameIdx = i
			}
		}
		rest := avail
		if nameIdx >= 0 {
			w[nameIdx] = min(w[nameIdx], avail*2/5)
			rest -= w[nameIdx]
		}
		lo, hi := minW, maxW
		for lo < hi {
			mid := (lo + hi + 1) / 2
			sum := 0
			for i, x := range w {
				if i != nameIdx {
					sum += min(x, mid)
				}
			}
			if sum <= rest {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		used := 0
		for i := range w {
			if i != nameIdx {
				w[i] = min(w[i], lo)
			}
			used += w[i]
		}
		if used < avail {
			if nameIdx < 0 {
				nameIdx = 0
			}
			w[nameIdx] += avail - used
		}
	} else if total < avail && len(w) > 0 {
		// give extra space to NAME (or the first column)
		idx := 0
		for i, c := range s.Columns {
			if c.Name == "NAME" {
				idx = i
				break
			}
		}
		w[idx] += avail - total
	}
	v.widths = w
	if len(v.headerClicks) < len(s.Columns) {
		v.headerClicks = make([]widget.Clickable, len(s.Columns))
	}
}

func (v *ResourceView) layoutHeader(gtx C, s *backend.Snapshot) D {
	th := v.app.th
	h := gtx.Dp(headerHeight)
	sz := image.Pt(gtx.Constraints.Max.X, h)
	fillRect(gtx, pal.Header, image.Rectangle{Max: sz})
	x := 0
	for i, c := range s.Columns {
		if i >= len(v.widths) {
			break
		}
		w := v.widths[i]
		cl := &v.headerClicks[i]
		if cl.Clicked(gtx) && v.watch != nil {
			v.watch.SetSort(c.Name)
		}
		name := c.Name
		sorted := s.SortCol == c.Name
		st := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		cg := gtx
		cg.Constraints = layout.Exact(image.Pt(w, h))
		cl.Layout(cg, func(gtx C) D {
			col := pal.TextDim
			if cl.Hovered() || s.SortCol == c.Name {
				col = pal.TextBright
			}
			return layout.Inset{Left: cellPad, Right: cellPad}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.W.Layout(gtx, func(gtx C) D {
					if sorted {
						k := iconChevronUp
						if !s.SortAsc {
							k = iconChevronDown
						}
						return labelIcon(gtx, th, 12, col, name, k)
					}
					return lbl(gtx, th, fontSans, 12, col, name)
				})
			})
		})
		st.Pop()
		x += w
	}
	st := op.Offset(image.Pt(0, h-gtx.Dp(1))).Push(gtx.Ops)
	hline(gtx, pal.Border)
	st.Pop()
	return D{Size: sz}
}

func (v *ResourceView) layoutRows(gtx C, s *backend.Snapshot) D {
	th := v.app.th
	rh := gtx.Dp(rowHeight)
	size := gtx.Constraints.Max

	// pointer handling on the whole rows area; the list's own scroll
	// handler is nested inside, so both receive events.
	area := clip.Rect{Max: size}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.rowsTag)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &v.rowsTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok || pe.Kind != pointer.Press {
			continue
		}
		gtx.Execute(key.FocusCmd{})
		idx := v.rowAt(pe.Position, rh)
		if idx < 0 || idx >= len(s.Rows) {
			continue
		}
		double := idx == v.lastClickRow && time.Since(v.lastClick) < 400*time.Millisecond
		v.lastClick, v.lastClickRow = time.Now(), idx
		v.setSel(idx)
		if pe.Buttons == pointer.ButtonSecondary || double {
			v.enter()
		}
	}

	if len(s.Rows) == 0 {
		area.Pop()
		return layout.Center.Layout(gtx, func(gtx C) D {
			msg := "No resources"
			if s.Err != nil {
				msg = s.Err.Error()
			}
			return lbl(gtx, th, fontSans, 14, pal.TextDim, msg)
		})
	}

	d := material.List(th, &v.list).Layout(gtx, len(s.Rows), func(gtx C, i int) D {
		return v.layoutRow(gtx, s, i, rh)
	})
	area.Pop()
	return d
}

func (v *ResourceView) rowAt(p f32.Point, rh int) int {
	pos := v.list.Position
	return pos.First + (int(p.Y)+pos.Offset)/rh
}

func (v *ResourceView) layoutRow(gtx C, s *backend.Snapshot, i, rh int) D {
	th := v.app.th
	r := s.Rows[i]
	sz := image.Pt(gtx.Constraints.Max.X, rh)
	switch {
	case i == v.sel:
		fillRect(gtx, pal.Selected, image.Rectangle{Max: sz})
	case i%2 == 1:
		fillRect(gtx, withAlpha(pal.Panel, 0x60), image.Rectangle{Max: sz})
	}
	fg := stateColor(r.State)
	if i == v.sel {
		fg = pal.TextBright
	}
	x := 0
	pad := gtx.Dp(cellPad)
	for c, cell := range r.Cells {
		if c >= len(v.widths) {
			break
		}
		w := v.widths[c]
		if cell != "" {
			st := op.Offset(image.Pt(x+pad, (rh-gtx.Sp(tableText+4))/2)).Push(gtx.Ops)
			cg := gtx
			cg.Constraints = layout.Exact(image.Pt(max(0, w-2*pad), rh))
			a := text.Start
			if s.Columns[c].AlignRight {
				a = text.End
			}
			col := fg
			if c == 0 && i != v.sel && r.State == backend.StateStd {
				col = pal.TextBright
			}
			lblAlign(cg, th, fontSans, tableText, col, cell, a)
			st.Pop()
		}
		x += w
	}
	return D{Size: sz}
}

func titleFor(gvr string) string {
	for _, sec := range sections {
		for _, it := range sec.items {
			if it.gvr == gvr {
				return it.title
			}
		}
	}
	parts := strings.Split(gvr, "/")
	return strings.ToUpper(parts[len(parts)-1][:1]) + parts[len(parts)-1][1:]
}

func nsLabel(ns string) string {
	if ns == "" {
		return "All"
	}
	return ns
}
