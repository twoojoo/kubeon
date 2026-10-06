package ui

import (
	"fmt"
	"image"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/giovanni/kubeon/internal/backend"
	"github.com/giovanni/kubeon/internal/desktop"
	"github.com/giovanni/kubeon/internal/prefs"
)

// ---- port-forwards ----

// openBrowser opens url with the desktop handler.
func openBrowser(url string) error { return desktop.Open(url) }

// forwardRef identifies a tunnel in the saved preferences.
func forwardRef(ctx, gvr, path string, local, remote int) prefs.PortForward {
	return prefs.PortForward{Context: ctx, GVR: gvr, Path: path, Local: local, Remote: remote}
}

// forwardTitle is how a tunnel is called in messages: its name, else addr.
func (a *App) forwardTitle(f prefs.PortForward, addr string) string {
	if n := a.prefs.ForwardLabel(f); n != "" {
		return n + " (" + addr + ")"
	}
	return addr
}

// startSavedForwards re-opens the pinned forwards of the context. They start
// without confirmation (the user pinned them) but are announced in one
// summary; on protected contexts also in the notification bell.
func (a *App) startSavedForwards(c *backend.Cluster) {
	var todo []prefs.PortForward
	for _, f := range a.prefs.SavedForwards(c.Context) {
		if f.AutoStart {
			todo = append(todo, f)
		}
	}
	if len(todo) == 0 {
		return
	}
	go func() {
		var opened, failed []string
		for _, f := range todo {
			if pf, err := c.PortForward(f.GVR, f.Path, f.Local, f.Remote); err != nil {
				failed = append(failed, fmt.Sprintf("%s (%v)", a.forwardTitle(f, fmt.Sprintf("%s:%d", f.Path, f.Remote)), err))
			} else {
				opened = append(opened, a.forwardTitle(f, pf.String()))
			}
		}
		a.post(func() {
			if a.cluster != c {
				return
			}
			if len(failed) > 0 {
				a.flash("Pinned port-forwards failed: "+strings.Join(failed, "; "), true)
			}
			if len(opened) == 0 {
				return
			}
			msg := fmt.Sprintf("Opened %d pinned tunnel(s): %s", len(opened), strings.Join(opened, ", "))
			if a.protected() {
				a.onHealth([]backend.HealthEvent{{Severity: backend.SevWarn, Title: "Tunnels opened on " + c.Context,
					Body: strings.Join(opened, "\n"), At: time.Now()}})
			}
			a.flash(msg, false)
		})
	}()
}

// confirmForward opens a tunnel; on protected contexts it asks first (no
// typed name: a tunnel changes nothing in the cluster, the risk is opening
// one into production by mistake). Read-only contexts allow forwards.
func (a *App) confirmForward(path string, remote int, start func()) {
	if !a.protected() {
		start()
		return
	}
	a.overlay = newConfirm(a, "Open tunnel",
		fmt.Sprintf("Open a tunnel to %s:%d on %s?\n\nTraffic through it reaches the cluster directly.", path, remote, a.cluster.Context),
		false, func(bool) { start() })
}

// pfRow merges an active forward and/or its saved entry.
type pfRow struct {
	active                    *backend.PortForward
	saved                     *prefs.PortForward
	open, pin, toggle, remove widget.Clickable
	name                      widget.Editor // the tunnel's label, saved as you type
}

func newPFRow() *pfRow {
	r := &pfRow{}
	r.name.SingleLine, r.name.Submit, r.name.MaxLen = true, true, prefs.MaxForwardName
	return r
}

// savedName is the name stored for the row ("" = none).
func (r *pfRow) savedName() string {
	if r.saved == nil {
		return ""
	}
	return r.saved.Name
}

func (r *pfRow) key() (gvr, path string, local, remote int) {
	if r.active != nil {
		return r.active.GVR, r.active.Target, r.active.Local, r.active.Remote
	}
	return r.saved.GVR, r.saved.Path, r.saved.Local, r.saved.Remote
}

// PortForwardPanel manages active and saved forwards.
type PortForwardPanel struct {
	app  *App
	done bool
	rows []*pfRow
	list widget.List
}

func newPortForwardPanel(a *App) *PortForwardPanel {
	p := &PortForwardPanel{app: a}
	p.list.Axis = layout.Vertical
	p.reload()
	return p
}

func (p *PortForwardPanel) Done() bool { return p.done }

func (p *PortForwardPanel) reload() {
	a := p.app
	if a.cluster == nil {
		return
	}
	old := map[string]*pfRow{}
	for _, r := range p.rows {
		g, path, _, rem := r.key()
		old[fmt.Sprintf("%s|%s|%d", g, path, rem)] = r
	}
	var rows []*pfRow
	seen := map[string]*pfRow{}
	for _, f := range a.cluster.PortForwards() {
		k := fmt.Sprintf("%s|%s|%d", f.GVR, f.Target, f.Remote)
		r := old[k]
		if r == nil {
			r = newPFRow()
		}
		r.active, r.saved = f, nil
		rows = append(rows, r)
		seen[k] = r
	}
	for _, s := range a.prefs.SavedForwards(a.cluster.Context) {
		s := s
		k := fmt.Sprintf("%s|%s|%d", s.GVR, s.Path, s.Remote)
		if r := seen[k]; r != nil {
			r.saved = &s
			continue
		}
		r := old[k]
		if r == nil {
			r = newPFRow()
		}
		r.active, r.saved = nil, &s
		rows = append(rows, r)
	}
	p.rows = rows
}

func (p *PortForwardPanel) Layout(gtx C) D {
	a, th := p.app, p.app.th
	c := a.cluster
	for _, r := range p.rows {
		r := r
		gvr, path, local, remote := r.key()
		if r.open.Clicked(gtx) && r.active != nil {
			if err := openBrowser(fmt.Sprintf("http://localhost:%d", r.active.Local)); err != nil {
				a.flash("open browser: "+err.Error(), true)
			}
		}
		// the name is saved as it is typed (also when the panel is dismissed)
		for {
			ev, ok := r.name.Update(gtx)
			if !ok {
				break
			}
			switch ev.(type) {
			case widget.SubmitEvent:
				gtx.Execute(key.FocusCmd{})
			case widget.ChangeEvent:
				p.rename(r, r.name.Text())
			}
		}
		if !gtx.Focused(&r.name) && r.name.Text() != r.savedName() {
			r.name.SetText(r.savedName()) // follow the saved value (after a reload, or a trim)
		}
		if r.pin.Clicked(gtx) {
			a.prefs.TogglePinPortForward(forwardRef(c.Context, gvr, path, local, remote))
			p.save()
			p.reload()
		}
		if r.toggle.Clicked(gtx) {
			if r.active != nil {
				c.StopPortForward(r.active.ID)
				p.reload()
			} else {
				a.confirmForward(path, remote, func() {
					a.overlay = p // back to the panel after a confirmation
					go func() {
						_, err := c.PortForward(gvr, path, local, remote)
						a.post(func() {
							if err != nil {
								a.flash("port-forward: "+err.Error(), true)
							}
							p.reload()
						})
					}()
				})
			}
		}
		if r.remove.Clicked(gtx) {
			if r.active != nil {
				c.StopPortForward(r.active.ID)
			}
			if r.saved != nil {
				a.prefs.RemovePortForward(*r.saved)
				p.save()
			}
			p.reload()
		}
	}
	w := min(gtx.Dp(640), gtx.Constraints.Max.X-gtx.Dp(40))
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
	return rborder(gtx, pal.Border, pal.Panel, 10, func(gtx C) D {
		return inset(16).Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return iconText(gtx, th, icForward, 16, pal.TextBright, "Port-forwards") }),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 4, Bottom: 10}.Layout(gtx, func(gtx C) D {
						return lbl(gtx, th, fontSans, 11, pal.TextDim, "Pin a tunnel to re-open it when this context connects. Give it a name to recognise it.")
					})
				}),
				layout.Rigid(func(gtx C) D {
					if len(p.rows) == 0 {
						return inset(16).Layout(gtx, func(gtx C) D {
							return lbl(gtx, th, fontSans, 13, pal.TextDim, "No port-forwards. Select a pod/service/deployment and press shift+f.")
						})
					}
					gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, gtx.Dp(420))
					return material.List(th, &p.list).Layout(gtx, len(p.rows), func(gtx C, i int) D {
						return p.layoutRow(gtx, p.rows[i])
					})
				}),
			)
		})
	})
}

func (p *PortForwardPanel) save() {
	if err := p.app.prefs.Save(); err != nil {
		p.app.flash("save preferences: "+err.Error(), true)
	}
}

// rename stores the name typed for row r (a tunnel never named before is
// remembered, unpinned; an emptied name of an unpinned tunnel is forgotten).
func (p *PortForwardPanel) rename(r *pfRow, name string) {
	a := p.app
	if a.cluster == nil || prefs.CleanForwardName(name) == r.savedName() {
		return
	}
	gvr, path, local, remote := r.key()
	a.prefs.SetPortForwardName(forwardRef(a.cluster.Context, gvr, path, local, remote), name)
	p.save()
	p.reload()
}

// nameField is the tunnel's name: an inline text field (hint when empty)
// underlined while hovered or focused.
func (p *PortForwardPanel) nameField(gtx C, r *pfRow) D {
	th := p.app.th
	return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		ed := material.Editor(th, &r.name, "Add a name…")
		ed.Font, ed.TextSize = fontSans, 13.5
		ed.Color, ed.HintColor, ed.SelectionColor = pal.TextBright, pal.TextDim, withAlpha(pal.Accent, 0x60)
		d := ed.Layout(gtx)
		call := m.Stop()
		call.Add(gtx.Ops)
		line := withAlpha(pal.Border, 0x00)
		switch {
		case gtx.Focused(&r.name):
			line = pal.Accent
		case r.savedName() == "":
			line = withAlpha(pal.Border, 0xa0) // invites typing
		}
		fillRect(gtx, line, image.Rect(0, d.Size.Y, d.Size.X, d.Size.Y+gtx.Dp(1)))
		return D{Size: image.Pt(d.Size.X, d.Size.Y+gtx.Dp(1))}
	})
}

func (p *PortForwardPanel) layoutRow(gtx C, r *pfRow) D {
	th := p.app.th
	gvr, path, local, remote := r.key()
	return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx C) D {
		return rbg(gtx, pal.Bg, 8, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return insetHV(10, 8).Layout(gtx, func(gtx C) D {
				st, sc := "stopped", pal.TextDim
				if r.active != nil {
					st, sc = "active", pal.Ok
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return dot(gtx, sc, 8) }),
					layout.Rigid(layout.Spacer{Width: 9}.Layout),
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return p.nameField(gtx, r) }),
							layout.Rigid(func(gtx C) D {
								return p.app.selLbl(gtx, fmt.Sprintf("pf/%s/%s/%d", gvr, path, remote), fontMono, 12, pal.Text, fmt.Sprintf("localhost:%d → :%d", local, remote))
							}),
							layout.Rigid(func(gtx C) D {
								return p.app.selLbl(gtx, fmt.Sprintf("pf/%s/%s/%d/sub", gvr, path, remote), fontSans, 11, pal.TextDim, singular(strings.ToLower(titleFor(gvr)))+" "+path+" · "+st)
							}),
						)
					}),
					layout.Rigid(func(gtx C) D {
						if r.active == nil {
							return D{}
						}
						return pillButton(gtx, th, &r.open, icOpen, "Open", pal.Text, colorNRGBA{})
					}),
					layout.Rigid(func(gtx C) D {
						ic, txt, c := icBookmark, "Pin", pal.TextDim
						if r.saved != nil && r.saved.AutoStart {
							txt, c = "Pinned", pal.Accent
						}
						return pillButton(gtx, th, &r.pin, ic, txt, c, colorNRGBA{})
					}),
					layout.Rigid(func(gtx C) D {
						ic, txt := icStop, "Stop"
						if r.active == nil {
							ic, txt = icJob, "Start"
						}
						return pillButton(gtx, th, &r.toggle, ic, txt, pal.Text, colorNRGBA{})
					}),
					layout.Rigid(func(gtx C) D { return iconOnly(gtx, &r.remove, icDelete, 15) }),
				)
			})
		})
	})
}

// ---- bookmarks ----

// bookmarkCurrent saves the current view (resource, namespace, filter).
func (a *App) bookmarkCurrent() {
	if a.cluster == nil || a.page == overviewGVR || a.page == xrayPage || a.view.gvr == "" {
		a.flash("open a resource table to bookmark it", true)
		return
	}
	gvr, ns, filter := a.view.gvr, a.ns, strings.TrimSpace(a.view.filter.Text())
	name := titleFor(gvr) + " · " + nsLabel(ns)
	if filter != "" {
		name += " · " + filter
	}
	ctx := a.cluster.Context
	p := newPicker(a, "Bookmark name (saved for context "+ctx+")", nil, true, func(n string) {
		n = strings.TrimSpace(n)
		if n == "" {
			return
		}
		a.prefs.AddBookmark(prefs.Bookmark{Name: n, Context: ctx, GVR: gvr, Namespace: ns, Filter: filter})
		if err := a.prefs.Save(); err != nil {
			a.flash("save bookmark: "+err.Error(), true)
			return
		}
		a.flash("Bookmarked "+n, false)
	})
	p.editor.SetText(name)
	p.icon = icBookmark
	a.overlay = p
}

// openBookmark restores a saved view.
func (a *App) openBookmark(b prefs.Bookmark) {
	a.ns = b.Namespace
	a.view.filter.SetText(b.Filter)
	a.view.lastFilter = ""
	a.openResource(b.GVR)
	a.view.applyFilter()
	a.sidebar.active = "bm:" + b.Name
}

// pickBookmark lists bookmarks (palette "bm").
func (a *App) pickBookmark() {
	if a.cluster == nil {
		return
	}
	bms := a.prefs.BookmarksFor(a.cluster.Context)
	if len(bms) == 0 {
		a.flash("no bookmarks yet — press m on a resource view", false)
		return
	}
	var items []string
	byName := map[string]prefs.Bookmark{}
	for _, b := range bms {
		items = append(items, b.Name)
		byName[b.Name] = b
	}
	p := newPicker(a, "Bookmarks", items, false, func(s string) { a.openBookmark(byName[s]) })
	p.icon = icBookmark
	a.overlay = p
}

// ---- tabs ----

// tabState is a saved main-area view.
type tabState struct {
	st     navState
	ns     string
	filter string
	title  string
	close  widget.Clickable
	click  widget.Clickable
}

func (a *App) snapshotTab() *tabState {
	return &tabState{st: a.current(), ns: a.ns, filter: a.view.filter.Text(), title: a.pageTitleSafe()}
}

func (a *App) ensureTabs() {
	if len(a.tabs) == 0 {
		a.tabs = []*tabState{a.snapshotTab()}
		a.tabIdx = 0
	}
}

// newTab duplicates the current view in a new tab.
func (a *App) newTab() {
	if a.cluster == nil {
		return
	}
	a.ensureTabs()
	a.tabs[a.tabIdx] = a.snapshotTab()
	t := a.snapshotTab()
	a.tabs = append(a.tabs, t)
	a.tabIdx = len(a.tabs) - 1
}

func (a *App) switchTab(i int) {
	if i < 0 || i >= len(a.tabs) || i == a.tabIdx {
		return
	}
	a.tabs[a.tabIdx] = a.snapshotTab()
	a.tabIdx = i
	t := a.tabs[i]
	a.ns = t.ns
	a.view.filter.SetText(t.filter)
	a.view.lastFilter = ""
	a.navStack = nil
	a.show(t.st)
	a.view.applyFilter()
}

func (a *App) closeTab(i int) {
	if len(a.tabs) <= 1 || i < 0 || i >= len(a.tabs) {
		return
	}
	cur := a.tabIdx
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	switch {
	case i == cur:
		next := min(i, len(a.tabs)-1)
		a.tabIdx = -1
		a.tabIdx = next
		t := a.tabs[next]
		a.ns = t.ns
		a.view.filter.SetText(t.filter)
		a.view.lastFilter = ""
		a.navStack = nil
		a.show(t.st)
		a.view.applyFilter()
	case i < cur:
		a.tabIdx--
	}
	if len(a.tabs) == 1 {
		a.tabs = nil
	}
}

func (a *App) cycleTab(d int) {
	if len(a.tabs) < 2 {
		return
	}
	a.switchTab((a.tabIdx + d + len(a.tabs)) % len(a.tabs))
}

// layoutTabs draws the tab strip (only with 2+ tabs).
func (a *App) layoutTabs(gtx C) D {
	if len(a.tabs) < 2 {
		return D{}
	}
	th := a.th
	a.tabs[a.tabIdx].title = a.pageTitleSafe()
	a.tabs[a.tabIdx].ns = a.ns
	for i, t := range a.tabs {
		if t.close.Clicked(gtx) {
			a.closeTab(i)
			return D{}
		}
		if t.click.Clicked(gtx) {
			a.switchTab(i)
		}
	}
	if a.newTabBtn.Clicked(gtx) {
		a.newTab()
	}
	h := gtx.Dp(32)
	fillRect(gtx, pal.Header, image.Rect(0, 0, gtx.Constraints.Max.X, h))
	fillRect(gtx, pal.Border, image.Rect(0, h-gtx.Dp(1), gtx.Constraints.Max.X, h))
	var ch []layout.FlexChild
	for i, t := range a.tabs {
		i, t := i, t
		ch = append(ch, layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(240))
			gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, h)
			active := i == a.tabIdx
			return t.click.Layout(gtx, func(gtx C) D {
				sz := gtx.Constraints.Min
				switch {
				case active:
					fillRect(gtx, pal.Panel, image.Rectangle{Max: sz})
					fillRect(gtx, pal.Accent, image.Rect(0, sz.Y-gtx.Dp(2), sz.X, sz.Y))
				case t.click.Hovered():
					fillRect(gtx, pal.Hover, image.Rectangle{Max: sz})
				}
				fillRect(gtx, pal.Border, image.Rect(sz.X-gtx.Dp(1), gtx.Dp(6), sz.X, sz.Y-gtx.Dp(6)))
				c := pal.TextDim
				if active {
					c = pal.TextBright
				}
				return insetHV(10, 0).Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return mdi(gtx, iconFor(t.st.gvr), c, 14) }),
						layout.Rigid(layout.Spacer{Width: 6}.Layout),
						layout.Flexed(1, func(gtx C) D {
							title := t.title
							if t.ns != "" {
								title += " · " + t.ns
							}
							gtx.Constraints.Min.Y = h
							return layout.W.Layout(gtx, func(gtx C) D { return lbl(gtx, th, fontSans, 12, c, title) })
						}),
						layout.Rigid(func(gtx C) D { return iconOnly(gtx, &t.close, icClose, 11) }),
					)
				})
			})
		}))
	}
	ch = append(ch, layout.Rigid(func(gtx C) D {
		gtx.Constraints.Min.Y = h
		return layout.Center.Layout(gtx, func(gtx C) D {
			return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D { return iconOnly(gtx, &a.newTabBtn, icAdd, 15) })
		})
	}))
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, ch...)
}
