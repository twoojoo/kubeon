package ui

import (
	"image"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/giovanni/kubeon/internal/backend"
)

const (
	xrayTree = iota
	xrayGraph
)

// xrow is a flattened tree row.
type xrow struct {
	n       *backend.XNode
	depth   int
	key     string
	hasKids bool
}

// xbox is a positioned graph node.
type xbox struct {
	n      *backend.XNode
	key    string
	r      image.Rectangle
	parent int
}

// XRayView shows k9s xray dependency trees as a collapsible tree or a
// left-to-right graph.
type XRayView struct {
	app               *App
	gvr, ns, focus    string
	watch             *backend.XRayWatch
	raw, root         *backend.XNode
	err               string
	mode              int
	collapsed         map[string]bool
	rows              []xrow
	list              widget.List
	sel               int
	selKey            string
	rowsTag           int
	lastClick         time.Time
	lastKey           string
	treeBtn, graphBtn widget.Clickable
	expBtn, colBtn    widget.Clickable
	backBtn           widget.Clickable

	// graph state
	boxes    []xbox
	pan      f32.Point
	dragging bool
	dragFrom f32.Point
	graphTag int
}

func newXRay(a *App) *XRayView {
	x := XRayView{app: a, collapsed: map[string]bool{}, sel: -1}
	x.list.Axis = layout.Vertical
	return &x
}

func (x *XRayView) open(gvr, ns, focus string) {
	x.close()
	x.gvr, x.ns, x.focus = gvr, ns, focus
	x.raw, x.root, x.err, x.rows = nil, nil, "", nil
	x.sel, x.selKey, x.pan = -1, "", f32.Point{}
	x.collapsed = map[string]bool{}
	var w *backend.XRayWatch
	w = x.app.cluster.XRay(gvr, ns, func(n *backend.XNode, err error) {
		x.app.post(func() {
			if x.watch != w { // queued by a closed watch
				return
			}
			if err != nil {
				x.err = err.Error()
				return
			}
			x.raw = n
			x.rebuild()
		})
	})
	x.watch = w
}

func (x *XRayView) close() {
	if x.watch != nil {
		x.watch.Close()
		x.watch = nil
	}
}

// prune drops the namespace nodes k9s inserts below each object.
func prune(n *backend.XNode, depth int) *backend.XNode {
	c := *n
	c.Children = nil
	for _, k := range n.Children {
		if k.Kind == "namespaces" && depth > 0 {
			for _, gk := range k.Children {
				c.Children = append(c.Children, prune(gk, depth+1))
			}
			continue
		}
		c.Children = append(c.Children, prune(k, depth+1))
	}
	return &c
}

func findNode(n *backend.XNode, id string) *backend.XNode {
	if n.ID == id && n.Kind != "namespaces" {
		return n
	}
	for _, k := range n.Children {
		if f := findNode(k, id); f != nil {
			return f
		}
	}
	return nil
}

func (x *XRayView) rebuild() {
	if x.raw == nil {
		return
	}
	root := prune(x.raw, 0)
	if x.focus != "" {
		if f := findNode(root, x.focus); f != nil {
			root = f
		}
	}
	x.root = root
	x.rows = x.rows[:0]
	var walk func(n *backend.XNode, depth int, key string)
	walk = func(n *backend.XNode, depth int, key string) {
		x.rows = append(x.rows, xrow{n: n, depth: depth, key: key, hasKids: len(n.Children) > 0})
		if x.collapsed[key] {
			return
		}
		for _, k := range n.Children {
			walk(k, depth+1, key+"/"+k.Kind+":"+k.ID)
		}
	}
	walk(root, 0, root.Kind+":"+root.ID)
	x.sel = -1
	for i, r := range x.rows {
		if r.key == x.selKey {
			x.sel = i
		}
	}
	x.layoutGraph()
}

func (x *XRayView) toggle(key string) {
	x.collapsed[key] = !x.collapsed[key]
	x.rebuild()
}

func (x *XRayView) setAll(collapsed bool) {
	x.collapsed = map[string]bool{}
	if collapsed {
		for _, r := range x.rows {
			if r.depth >= 1 && r.hasKids {
				x.collapsed[r.key] = true
			}
		}
	}
	x.rebuild()
}

func (x *XRayView) selectRow(i int) {
	if i < 0 || i >= len(x.rows) {
		return
	}
	x.sel, x.selKey = i, x.rows[i].key
	n := x.rows[i].n
	if x.app.drawer.open && isResource(n) {
		x.app.drawer.follow(n.GVR, n.ID)
	}
}

func isResource(n *backend.XNode) bool {
	return strings.Contains(n.GVR, "/") && n.Kind != "namespaces"
}

func (x *XRayView) openDetails() {
	if x.sel < 0 || x.sel >= len(x.rows) {
		return
	}
	n := x.rows[x.sel].n
	if isResource(n) {
		x.app.drawer.show(n.GVR, n.ID, tabOverview, false)
	}
}

func (x *XRayView) onKey(e key.Event) {
	switch e.Name {
	case key.NameUpArrow, "K":
		x.selectRow(max(0, x.sel-1))
	case key.NameDownArrow, "J":
		x.selectRow(min(len(x.rows)-1, x.sel+1))
	case key.NameRightArrow, key.NameLeftArrow, key.NameSpace:
		if x.sel >= 0 && x.rows[x.sel].hasKids {
			c := x.collapsed[x.rows[x.sel].key]
			if (e.Name == key.NameRightArrow && c) || (e.Name == key.NameLeftArrow && !c) || e.Name == key.NameSpace {
				x.toggle(x.rows[x.sel].key)
			}
		}
	case key.NameReturn, key.NameEnter, "D":
		x.openDetails()
	}
}

func statusColor(n *backend.XNode) colorNRGBA {
	switch {
	case n.Status == "" || n.Status == "ok":
		return pal.Ok
	case strings.Contains(n.Status, "kids"):
		return pal.Warn
	case n.Status == "noref":
		return pal.Warn
	default:
		return pal.Err
	}
}

func kindIcon(k string) *widget.Icon {
	if ic, ok := kindIcons[k]; ok {
		return ic
	}
	return icCRD
}

func (x *XRayView) Layout(gtx C) D {
	th := x.app.th
	if x.treeBtn.Clicked(gtx) {
		x.mode = xrayTree
	}
	if x.graphBtn.Clicked(gtx) {
		x.mode = xrayGraph
	}
	if x.expBtn.Clicked(gtx) {
		x.setAll(false)
	}
	if x.colBtn.Clicked(gtx) {
		x.setAll(true)
	}
	if x.backBtn.Clicked(gtx) {
		x.app.back()
		return D{}
	}
	fill(gtx, pal.Bg)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return bg(gtx, pal.Panel, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Inset{Left: 12, Right: 14, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							if len(x.app.navStack) == 0 {
								return D{}
							}
							return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D { return iconOnly(gtx, &x.backBtn, icBack, 18) })
						}),
						layout.Rigid(func(gtx C) D {
							return rbg(gtx, withAlpha(pal.Accent, 0x30), 7, func(gtx C) D {
								return inset(6).Layout(gtx, func(gtx C) D { return mdi(gtx, icXRay, pal.Accent, 20) })
							})
						}),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 18, pal.TextBright, "XRay") }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(func(gtx C) D {
							s := titleFor(x.gvr)
							if x.focus != "" {
								s += "  ·  " + x.focus
							}
							return badge(gtx, th, s, pal.Text, pal.Bg)
						}),
						layout.Rigid(layout.Spacer{Width: 8}.Layout),
						layout.Rigid(func(gtx C) D {
							if x.root == nil {
								return D{}
							}
							return badge(gtx, th, itoa(x.root.Count())+" nodes", pal.TextDim, pal.Bg)
						}),
						layout.Flexed(1, layout.Spacer{}.Layout),
						layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &x.expBtn, icExpand, "Expand", pal.Text, colorNRGBA{}) }),
						layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &x.colBtn, icCollapse, "Collapse", pal.Text, colorNRGBA{}) }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(func(gtx C) D { return x.modeSwitch(gtx) }),
					)
				})
			})
		}),
		layout.Rigid(func(gtx C) D { return hline(gtx, pal.Border) }),
		layout.Flexed(1, func(gtx C) D {
			switch {
			case x.err != "" && x.root == nil:
				return layout.Center.Layout(gtx, func(gtx C) D { return lbl(gtx, th, fontSans, 14, pal.Err, x.err) })
			case x.root == nil:
				return layout.Center.Layout(gtx, func(gtx C) D { return lbl(gtx, th, fontSans, 14, pal.TextDim, "Building dependency tree…") })
			case x.mode == xrayGraph:
				return x.layoutGraphView(gtx)
			}
			return x.layoutTree(gtx)
		}),
	)
}

func (x *XRayView) modeSwitch(gtx C) D {
	th := x.app.th
	seg := func(cl *widget.Clickable, ic *widget.Icon, txt string, on bool) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			b, c := colorNRGBA{}, pal.TextDim
			if on {
				b, c = pal.Selected, pal.TextBright
			}
			return pillButton(gtx, th, cl, ic, txt, c, b)
		})
	}
	return rborder(gtx, pal.Border, pal.Bg, 7, func(gtx C) D {
		return inset(2).Layout(gtx, func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				seg(&x.treeBtn, icTree, "Tree", x.mode == xrayTree),
				seg(&x.graphBtn, icXRay, "Graph", x.mode == xrayGraph),
			)
		})
	})
}

// ---- tree ----

func (x *XRayView) layoutTree(gtx C) D {
	th := x.app.th
	rh := gtx.Dp(30)
	size := gtx.Constraints.Max
	area := clip.Rect{Max: size}.Push(gtx.Ops)
	event.Op(gtx.Ops, &x.rowsTag)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &x.rowsTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		gtx.Execute(key.FocusCmd{})
		pos := x.list.Position
		i := pos.First + (int(pe.Position.Y)+pos.Offset)/rh
		if i < 0 || i >= len(x.rows) {
			continue
		}
		r := x.rows[i]
		// click on the chevron area toggles
		chevX := gtx.Dp(12) + r.depth*gtx.Dp(22)
		if r.hasKids && int(pe.Position.X) < chevX+gtx.Dp(20) {
			x.toggle(r.key)
			continue
		}
		double := r.key == x.lastKey && time.Since(x.lastClick) < 400*time.Millisecond
		x.lastClick, x.lastKey = time.Now(), r.key
		x.selectRow(i)
		if double || pe.Buttons == pointer.ButtonSecondary {
			x.openDetails()
		}
	}
	d := material.List(th, &x.list).Layout(gtx, len(x.rows), func(gtx C, i int) D {
		return x.layoutTreeRow(gtx, i, rh)
	})
	area.Pop()
	return d
}

func (x *XRayView) layoutTreeRow(gtx C, i, rh int) D {
	th := x.app.th
	r := x.rows[i]
	sz := image.Pt(gtx.Constraints.Max.X, rh)
	if i == x.sel {
		fillRect(gtx, pal.Selected, image.Rectangle{Max: sz})
		fillRect(gtx, pal.Accent, image.Rect(0, 0, gtx.Dp(3), rh))
	}
	indent := gtx.Dp(22)
	left := gtx.Dp(12)
	// indentation guides
	for dpt := 1; dpt <= r.depth; dpt++ {
		gx := left + (dpt-1)*indent + gtx.Dp(9)
		fillRect(gtx, withAlpha(pal.Border, 0xc0), image.Rect(gx, 0, gx+gtx.Dp(1), rh))
	}
	x0 := left + r.depth*indent
	st := op.Offset(image.Pt(x0, 0)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(max(0, sz.X-x0-gtx.Dp(12)), rh))
	layout.W.Layout(g, func(gtx C) D {
		n := r.n
		sc := statusColor(n)
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				if !r.hasKids {
					return D{Size: image.Pt(gtx.Dp(18), gtx.Dp(18))}
				}
				return mdi(gtx, chevronIcon(x.collapsed[r.key]), pal.TextDim, 18)
			}),
			layout.Rigid(layout.Spacer{Width: 4}.Layout),
			layout.Rigid(func(gtx C) D { return mdi(gtx, kindIcon(n.Kind), sc, 16) }),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 13, pal.TextBright, n.Name) }),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 11, pal.TextDim, singular(n.Kind)) }),
			layout.Flexed(1, layout.Spacer{}.Layout),
			layout.Rigid(func(gtx C) D {
				if n.Info == "" {
					return D{}
				}
				return layout.Inset{Right: 10}.Layout(gtx, func(gtx C) D {
					return lbl(gtx, th, fontMono, 12, pal.Text, n.Info)
				})
			}),
			layout.Rigid(func(gtx C) D {
				if len(n.Children) > 0 && x.collapsed[r.key] {
					return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D {
						return badge(gtx, th, "+"+itoa(n.Count()-1), pal.TextDim, pal.Panel)
					})
				}
				return D{}
			}),
			layout.Rigid(func(gtx C) D { return dot(gtx, sc, 8) }),
		)
	})
	st.Pop()
	return D{Size: sz}
}

func singular(k string) string {
	switch {
	case strings.HasSuffix(k, "sses"):
		return strings.TrimSuffix(k, "es")
	case strings.HasSuffix(k, "ies"):
		return strings.TrimSuffix(k, "ies") + "y"
	case strings.HasSuffix(k, "s"):
		return strings.TrimSuffix(k, "s")
	}
	return k
}

// ---- graph ----

const (
	gBoxW  = unit.Dp(210)
	gBoxH  = unit.Dp(46)
	gColG  = unit.Dp(64)
	gRowG  = unit.Dp(10)
	gPadXY = unit.Dp(24)
)

// layoutGraph computes a tidy left→right layered layout in Dp units
// (converted to px at draw time): leaves stack vertically, parents center
// on their children.
func (x *XRayView) layoutGraph() {
	x.boxes = x.boxes[:0]
	if x.root == nil {
		return
	}
	leaf := 0
	var place func(n *backend.XNode, depth int, key string, parent int) (top, bot int)
	place = func(n *backend.XNode, depth int, key string, parent int) (int, int) {
		idx := len(x.boxes)
		x.boxes = append(x.boxes, xbox{n: n, key: key, parent: parent})
		bx := int(gPadXY) + depth*int(gBoxW+gColG)
		kids := n.Children
		if x.collapsed[key] {
			kids = nil
		}
		var y int
		if len(kids) == 0 {
			y = int(gPadXY) + leaf*int(gBoxH+gRowG)
			leaf++
		} else {
			first, last := -1, -1
			for _, k := range kids {
				ci := len(x.boxes)
				place(k, depth+1, key+"/"+k.Kind+":"+k.ID, idx)
				cy := x.boxes[ci].r.Min.Y
				if first < 0 {
					first = cy
				}
				last = cy
			}
			y = (first + last) / 2
		}
		x.boxes[idx].r = image.Rect(bx, y, bx+int(gBoxW), y+int(gBoxH))
		return y, y + int(gBoxH)
	}
	place(x.root, 0, x.root.Kind+":"+x.root.ID, -1)
}

func (x *XRayView) layoutGraphView(gtx C) D {
	th := x.app.th
	size := gtx.Constraints.Max
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &x.graphTag)
	pointer.CursorGrab.Add(gtx.Ops)
	sc := gtx.Metric.PxPerDp
	px := func(r image.Rectangle) image.Rectangle {
		return image.Rect(int(float32(r.Min.X)*sc), int(float32(r.Min.Y)*sc), int(float32(r.Max.X)*sc), int(float32(r.Max.Y)*sc)).
			Add(image.Pt(int(x.pan.X), int(x.pan.Y)))
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  &x.graphTag,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Scroll,
			ScrollX: pointer.ScrollRange{Min: -1e6, Max: 1e6},
			ScrollY: pointer.ScrollRange{Min: -1e6, Max: 1e6},
		})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			gtx.Execute(key.FocusCmd{})
			hit := -1
			for i, b := range x.boxes {
				if pe.Position.Round().In(px(b.r)) {
					hit = i
				}
			}
			if hit >= 0 {
				b := x.boxes[hit]
				double := b.key == x.lastKey && time.Since(x.lastClick) < 400*time.Millisecond
				x.lastClick, x.lastKey = time.Now(), b.key
				for i, r := range x.rows {
					if r.key == b.key {
						x.selectRow(i)
					}
				}
				x.selKey = b.key
				if double && len(b.n.Children) > 0 {
					x.toggle(b.key)
				} else if pe.Buttons == pointer.ButtonSecondary {
					x.openDetails()
				}
				continue
			}
			x.dragging, x.dragFrom = true, pe.Position
		case pointer.Drag:
			if x.dragging {
				x.pan = x.pan.Add(pe.Position.Sub(x.dragFrom))
				x.dragFrom = pe.Position
			}
		case pointer.Release:
			x.dragging = false
		case pointer.Scroll:
			if pe.Modifiers.Contain(key.ModShift) {
				x.pan.X -= pe.Scroll.Y
			} else {
				x.pan.X -= pe.Scroll.X
				x.pan.Y -= pe.Scroll.Y
			}
		}
	}

	// edges first
	for _, b := range x.boxes {
		if b.parent < 0 {
			continue
		}
		pr, cr := px(x.boxes[b.parent].r), px(b.r)
		from := f32.Pt(float32(pr.Max.X), float32(pr.Min.Y+pr.Dy()/2))
		to := f32.Pt(float32(cr.Min.X), float32(cr.Min.Y+cr.Dy()/2))
		dx := (to.X - from.X) / 2
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(from)
		p.CubeTo(f32.Pt(from.X+dx, from.Y), f32.Pt(to.X-dx, to.Y), to)
		c := withAlpha(pal.TextDim, 0x90)
		if b.key == x.selKey || x.boxes[b.parent].key == x.selKey {
			c = pal.Accent
		}
		paint.FillShape(gtx.Ops, c, clip.Stroke{Path: p.End(), Width: float32(gtx.Dp(1.5))}.Op())
	}
	// boxes
	view := image.Rectangle{Max: size}
	for _, b := range x.boxes {
		r := px(b.r)
		if !r.Overlaps(view) {
			continue
		}
		x.drawBox(gtx, th, b, r)
	}
	return D{Size: size}
}

func (x *XRayView) drawBox(gtx C, th *material.Theme, b xbox, r image.Rectangle) {
	n := b.n
	sc := statusColor(n)
	rad := gtx.Dp(8)
	border := pal.Border
	if b.key == x.selKey {
		border = pal.Accent
	}
	fillRRect(gtx, border, r, rad)
	fillRRect(gtx, pal.Panel, r.Inset(gtx.Dp(1)), rad-gtx.Dp(1))
	fillRRect(gtx, sc, image.Rect(r.Min.X+gtx.Dp(1), r.Min.Y+gtx.Dp(8), r.Min.X+gtx.Dp(4), r.Max.Y-gtx.Dp(8)), gtx.Dp(2))

	st := op.Offset(r.Min.Add(image.Pt(gtx.Dp(12), 0))).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(r.Dx()-gtx.Dp(20), r.Dy()))
	layout.W.Layout(g, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return rbg(gtx, withAlpha(sc, 0x26), 6, func(gtx C) D {
					return inset(5).Layout(gtx, func(gtx C) D { return mdi(gtx, kindIcon(n.Kind), sc, 16) })
				})
			}),
			layout.Rigid(layout.Spacer{Width: 9}.Layout),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 13, pal.TextBright, n.Name) }),
					layout.Rigid(func(gtx C) D {
						s := singular(n.Kind)
						if n.Info != "" {
							s += "  ·  " + n.Info
						}
						if len(n.Children) > 0 && x.collapsed[b.key] {
							s += "  ·  +" + itoa(n.Count()-1)
						}
						return lbl(gtx, th, fontSans, 11, pal.TextDim, s)
					}),
				)
			}),
		)
	})
	st.Pop()
}
