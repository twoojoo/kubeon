package ui

import (
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
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/giovanni/kubeon/internal/backend"
)

const (
	rowHeight    = unit.Dp(30)
	headerHeight = unit.Dp(32)
	cellPad      = unit.Dp(10)
	tableText    = unit.Sp(13)
)

// ResourceView is the main resource table: a virtualized list over the
// latest backend snapshot. Only visible rows are laid out each frame.
type ResourceView struct {
	app   *App
	gvr   string
	ns    string
	path  string
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
	marked       map[string]bool
	bulkBtns     bulkButtons
	hover        int
	hoverX       int // pointer x in the rows area (tooltip column)
	lastClick    time.Time
	lastClickRow int

	filter     widget.Editor
	lastFilter string
	wide       bool

	cordonBtn, drainBtn, xrayBtn, decodeBtn, valuesBtn, historyBtn, rollbackBtn, backBtn      widget.Clickable
	clearBtn, pfBtn, nsBtn, logsBtn, shellBtn, yamlBtn, descBtn, delBtn, scaleBtn, restartBtn widget.Clickable
}

func newResourceView(a *App) *ResourceView {
	v := ResourceView{app: a, sel: -1, hover: -1}
	v.list.Axis = layout.Vertical
	v.filter.SingleLine = true
	v.filter.Submit = true
	return &v
}

func (v *ResourceView) open(gvr, ns string) { v.openPath(gvr, ns, "") }

// openPath opens gvr scoped to an owner path (e.g. helm history).
func (v *ResourceView) openPath(gvr, ns, path string) {
	v.close()
	v.gvr, v.ns, v.path = gvr, ns, path
	v.marked = nil
	v.title = titleFor(gvr)
	if path != "" {
		v.title += "  ·  " + path
	}
	v.snap, v.sel, v.selID = nil, -1, ""
	v.widths = nil
	v.list.Position = layout.Position{}
	c := v.app.cluster
	var tw *backend.TableWatch
	tw = c.WatchPath(gvr, ns, path, func(s *backend.Snapshot) {
		v.app.post(func() {
			if v.watch == tw { // drop snapshots queued by a closed watch
				v.onSnapshot(s)
			}
		})
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
	// no table: keys must not act on rows that are no longer shown
	v.snap, v.sel, v.selID, v.marked = nil, -1, "", nil
}

// selectID moves the cursor to the row with id; false if it is gone.
func (v *ResourceView) selectID(id string) bool {
	if v.snap == nil {
		return false
	}
	for i := range v.snap.Rows {
		if v.snap.Rows[i].ID == id {
			v.setSel(i)
			return true
		}
	}
	return false
}

func (v *ResourceView) onSnapshot(s *backend.Snapshot) {
	if v.watch == nil {
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
	if v.cordonBtn.Clicked(gtx) {
		v.toggleCordon()
	}
	if v.drainBtn.Clicked(gtx) {
		v.drain()
	}
	if v.xrayBtn.Clicked(gtx) {
		v.xray()
	}
	if v.decodeBtn.Clicked(gtx) {
		v.details(tabData)
	}
	if v.valuesBtn.Clicked(gtx) {
		v.details(tabValues)
	}
	if v.historyBtn.Clicked(gtx) {
		v.helmHistory()
	}
	if v.rollbackBtn.Clicked(gtx) {
		v.rollback()
	}
	if v.backBtn.Clicked(gtx) {
		v.app.back()
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
	return bg(gtx, pal.Panel, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 16, Right: 14, Top: 10, Bottom: 8}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							if len(v.app.navStack) == 0 {
								return D{}
							}
							return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D { return iconOnly(gtx, &v.backBtn, icBack, 18) })
						}),
						layout.Rigid(func(gtx C) D {
							return rbg(gtx, withAlpha(pal.Accent, 0x30), 7, func(gtx C) D {
								return inset(6).Layout(gtx, func(gtx C) D { return mdi(gtx, iconFor(v.gvr), pal.Accent, 20) })
							})
						}),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(func(gtx C) D {
							return lbl(gtx, th, fontSans, 18, pal.TextBright, v.title)
						}),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(func(gtx C) D {
							s := v.snap
							if s == nil || s.Loading {
								return badge(gtx, th, "loading…", pal.TextDim, pal.Bg)
							}
							n := itoa(len(s.Rows))
							if len(s.Rows) != s.Total {
								n += " / " + itoa(s.Total)
							}
							return badge(gtx, th, n, pal.Text, pal.Bg)
						}),
						layout.Flexed(1, layout.Spacer{}.Layout),
						layout.Rigid(func(gtx C) D {
							if !backend.IsNamespaced(v.gvr) {
								return D{}
							}
							return v.nsBtn.Layout(gtx, func(gtx C) D {
								b := pal.Border
								if v.nsBtn.Hovered() {
									b = pal.TextDim
								}
								return rborder(gtx, b, pal.Bg, 6, func(gtx C) D {
									return insetHV(10, 6).Layout(gtx, func(gtx C) D {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx C) D { return mdi(gtx, icNamespace, pal.TextDim, 15) }),
											layout.Rigid(layout.Spacer{Width: 7}.Layout),
											layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 13, pal.TextBright, nsLabel(v.ns)) }),
											layout.Rigid(layout.Spacer{Width: 4}.Layout),
											layout.Rigid(func(gtx C) D { return mdi(gtx, icExpand, pal.TextDim, 16) }),
										)
									})
								})
							})
						}),
						layout.Rigid(layout.Spacer{Width: 8}.Layout),
						layout.Rigid(func(gtx C) D {
							w := min(gtx.Dp(300), gtx.Constraints.Max.X)
							gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
							return v.layoutFilter(gtx)
						}),
					)
				})
			}),
			layout.Rigid(v.layoutActions),
		)
	})
}

// layoutActions is the contextual action bar for the selected object.
func (v *ResourceView) layoutActions(gtx C) D {
	if len(v.markedIDs()) > 1 {
		return v.layoutBulkBar(gtx)
	}
	row, ok := v.selected()
	if !ok {
		return D{}
	}
	th := v.app.th
	c := v.app.cluster
	items := []layout.FlexChild{
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return dot(gtx, stateColor(row.State), 8) }),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(360))
					return lbl(gtx, th, fontSans, 13, pal.TextBright, row.ID)
				}),
			)
		}),
		layout.Flexed(1, layout.Spacer{}.Layout),
	}
	compact := gtx.Constraints.Max.X < gtx.Dp(900)
	add := func(cl *widget.Clickable, ic *widget.Icon, txt string, show bool, danger bool) {
		if !show {
			return
		}
		if compact {
			txt = ""
		}
		fg := pal.Text
		if danger {
			fg = pal.Err
		}
		items = append(items, layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D {
				return pillButton(gtx, th, cl, ic, txt, fg, colorNRGBA{})
			})
		}))
	}
	isPod := v.gvr == "v1/pods"
	helm := isHelm(v.gvr)
	rw := !v.app.readOnly()
	isNode := v.gvr == "v1/nodes"
	if isNode {
		ic, txt := icCordon, "Cordon"
		if nodeCordoned(row) {
			ic, txt = icUncordon, "Uncordon"
		}
		add(&v.cordonBtn, ic, txt, rw, false)
		add(&v.drainBtn, icDrain, "Drain", rw, true)
	}
	add(&v.logsBtn, icLogs, "Logs", c.IsLoggable(v.gvr), false)
	add(&v.shellBtn, icTerminal, "Shell", isPod && rw, false)
	add(&v.pfBtn, icForward, "Forward", canForward(v.gvr), false)
	add(&v.xrayBtn, icXRay, "XRay", backend.CanXRay(v.gvr), false)
	add(&v.decodeBtn, icSecret, "Decode", v.gvr == "v1/secrets", false)
	add(&v.historyBtn, icHistory, "History", v.gvr == backend.HelmGVR, false)
	add(&v.valuesBtn, icValues, "Values", helm, false)
	add(&v.rollbackBtn, icRollback, "Rollback", rw && v.gvr == backend.HelmHistoryGVR, true)
	add(&v.descBtn, icDescribe, "Describe", true, false)
	add(&v.yamlBtn, icEdit, "YAML", !helm, false)
	add(&v.scaleBtn, icScale, "Scale", rw && c.CanScale(v.gvr), false)
	add(&v.restartBtn, icRestart, "Restart", rw && c.CanRestart(v.gvr), false)
	delTxt := "Delete"
	if v.gvr == backend.HelmGVR {
		delTxt = "Uninstall"
	}
	add(&v.delBtn, icDelete, delTxt, rw && (v.gvr == backend.HelmGVR || (!helm && backend.CanVerb(v.gvr, "delete"))), true)
	return bg(gtx, pal.Header, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Inset{Left: 16, Right: 10, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, items...)
		})
	})
}

func (v *ResourceView) layoutFilter(gtx C) D {
	th := v.app.th
	if v.clearBtn.Clicked(gtx) {
		v.filter.SetText("")
		v.applyFilter()
	}
	border := pal.Border
	if gtx.Focused(&v.filter) {
		border = pal.Accent
	}
	return rborder(gtx, border, pal.Bg, 6, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return insetHV(9, 6).Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return mdi(gtx, icSearch, pal.TextDim, 16) }),
				layout.Rigid(layout.Spacer{Width: 7}.Layout),
				layout.Flexed(1, func(gtx C) D {
					e := material.Editor(th, &v.filter, "Filter  (regex · -f fuzzy · -l label=…)")
					e.TextSize = 13
					e.Color = pal.TextBright
					e.HintColor = pal.TextDim
					return e.Layout(gtx)
				}),
				layout.Rigid(func(gtx C) D {
					if v.filter.Len() == 0 {
						return D{}
					}
					return iconOnly(gtx, &v.clearBtn, icClose, 12)
				}),
			)
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
	charW := float32(gtx.Sp(tableText)) * 0.64
	pad := gtx.Dp(cellPad)*2 + gtx.Dp(4)
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
		if isStatusCol(s.Columns[i].Name) {
			w[i] += gtx.Dp(28) // pill padding + dot
		}
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
		fixed := func(i int) bool { return i == nameIdx || isStatusCol(s.Columns[i].Name) }
		for i := range w {
			if i != nameIdx && fixed(i) {
				rest -= w[i]
			}
		}
		lo, hi := minW, maxW
		for lo < hi {
			mid := (lo + hi + 1) / 2
			sum := 0
			for i, x := range w {
				if !fixed(i) {
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
			if !fixed(i) {
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
						ic := icUp
						if !s.SortAsc {
							ic = icDown
						}
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 11, col, name) }),
							layout.Rigid(layout.Spacer{Width: 4}.Layout),
							layout.Rigid(func(gtx C) D { return mdi(gtx, ic, pal.Accent, 12) }),
						)
					}
					return lbl(gtx, th, fontSans, 11, col, name)
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
		ev, ok := gtx.Event(pointer.Filter{Target: &v.rowsTag, Kinds: pointer.Press | pointer.Move | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Move:
			v.hover = v.rowAt(pe.Position, rh)
			v.hoverX = int(pe.Position.X)
			continue
		case pointer.Leave, pointer.Cancel:
			v.hover = -1
			continue
		}
		gtx.Execute(key.FocusCmd{})
		idx := v.rowAt(pe.Position, rh)
		if idx < 0 || idx >= len(s.Rows) {
			continue
		}
		switch {
		case pe.Buttons == pointer.ButtonSecondary:
			if !v.marked[s.Rows[idx].ID] {
				v.clearMarks()
			}
			v.setSel(idx)
			v.openContextMenu(v.app.pointer)
			continue
		case pe.Modifiers.Contain(key.ModShortcut):
			v.setSel(idx)
			v.toggleMark(s.Rows[idx].ID)
			continue
		case pe.Modifiers.Contain(key.ModShift) && v.sel >= 0:
			lo, hi := min(v.sel, idx), max(v.sel, idx)
			for i := lo; i <= hi; i++ {
				v.mark(s.Rows[i].ID, true)
			}
			v.setSel(idx)
			continue
		}
		double := idx == v.lastClickRow && time.Since(v.lastClick) < 400*time.Millisecond
		v.lastClick, v.lastClickRow = time.Now(), idx
		v.setSel(idx)
		if double {
			v.enter()
		}
	}

	if len(s.Rows) == 0 {
		area.Pop()
		return layout.Center.Layout(gtx, func(gtx C) D {
			msg, ic, c := "No "+strings.ToLower(v.title)+" found", iconFor(v.gvr), pal.TextDim
			if s.Err != nil {
				msg, ic, c = s.Err.Error(), icError, pal.Err
			} else if v.filter.Len() > 0 {
				msg, ic = "Nothing matches the filter", icSearch
			}
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return mdi(gtx, ic, withAlpha(c, 0x80), 48) }),
				layout.Rigid(layout.Spacer{Height: 10}.Layout),
				layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 14, c, msg) }),
			)
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
	sel := i == v.sel
	marked := v.marked[r.ID]
	switch {
	case sel:
		fillRect(gtx, pal.Selected, image.Rectangle{Max: sz})
		fillRect(gtx, pal.Accent, image.Rect(0, 0, gtx.Dp(3), rh))
	case marked:
		fillRect(gtx, withAlpha(pal.Accent, 0x30), image.Rectangle{Max: sz})
	case i == v.hover:
		fillRect(gtx, pal.Hover, image.Rectangle{Max: sz})
	}
	if marked {
		fillRect(gtx, pal.Accent, image.Rect(0, 0, gtx.Dp(3), rh))
	}
	fillRect(gtx, withAlpha(pal.Border, 0x70), image.Rect(0, rh-gtx.Dp(1), sz.X, rh))
	if !sel && (r.State == backend.StateErr || r.State == backend.StatePending) {
		fillRect(gtx, stateColor(r.State), image.Rect(0, gtx.Dp(6), gtx.Dp(3), rh-gtx.Dp(6)))
	}
	fg := stateColor(r.State)
	if sel {
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
			st := op.Offset(image.Pt(x+pad, 0)).Push(gtx.Ops)
			cg := gtx
			cg.Constraints = layout.Exact(image.Pt(max(0, w-2*pad), rh))
			col := fg
			if c == 0 && !sel && r.State == backend.StateStd {
				col = pal.TextBright
			}
			text := func(gtx C) D { return lbl(gtx, th, fontSans, tableText, col, cell) }
			name := s.Columns[c].Name
			tone, toned := statusTone(cell)
			// hovered cell cut with an ellipsis: offer the full text
			if i == v.hover && v.hoverX >= x && v.hoverX < x+w && !(isStatusCol(name) && toned) &&
				truncated(gtx, th, fontSans, tableText, cell, w-2*pad) {
				v.app.hint(v.gvr+"|"+r.ID+"|"+name, cell)
			}
			switch {
			case isStatusCol(name) && toned:
				layout.W.Layout(cg, func(gtx C) D { return pill(gtx, th, cell, tone) })
			case name == "READY" && !sel && notReady(cell) && r.State != backend.StateCompleted:
				col = pal.Warn
				layout.W.Layout(cg, text)
			case s.Columns[c].AlignRight:
				layout.E.Layout(cg, text)
			default:
				layout.W.Layout(cg, text)
			}
			st.Pop()
		}
		x += w
	}
	return D{Size: sz}
}

func isStatusCol(n string) bool {
	switch n {
	case "STATUS", "PHASE", "STATE", "HEALTH":
		return true
	}
	return false
}

func notReady(s string) bool {
	a, b, ok := strings.Cut(s, "/")
	return ok && a != b
}

func titleFor(gvr string) string {
	switch gvr {
	case backend.HelmGVR:
		return "Helm Releases"
	case backend.HelmHistoryGVR:
		return "Release History"
	case "containers":
		return "Container"
	}
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
