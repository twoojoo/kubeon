package ui

import (
	"github.com/giovanni/k9desk/internal/prefs"
	"image"
	"sort"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/giovanni/k9desk/internal/backend"
)

type navItem struct {
	title, gvr string
}

type navSection struct {
	title string
	items []navItem
}

// sections mirrors Lens' navigation tree.
var sections = []navSection{
	{"Cluster", []navItem{
		{"Overview", overviewGVR},
		{"Sanitizer", "sanitizer"},
		{"Nodes", "v1/nodes"},
		{"Namespaces", "v1/namespaces"},
		{"Events", "events.k8s.io/v1/events"},
	}},
	{"Workloads", []navItem{
		{"Pods", "v1/pods"},
		{"Deployments", "apps/v1/deployments"},
		{"DaemonSets", "apps/v1/daemonsets"},
		{"StatefulSets", "apps/v1/statefulsets"},
		{"ReplicaSets", "apps/v1/replicasets"},
		{"Jobs", "batch/v1/jobs"},
		{"CronJobs", "batch/v1/cronjobs"},
	}},
	{"Config", []navItem{
		{"ConfigMaps", "v1/configmaps"},
		{"Secrets", "v1/secrets"},
		{"Resource Quotas", "v1/resourcequotas"},
		{"Limit Ranges", "v1/limitranges"},
		{"HPA", "autoscaling/v2/horizontalpodautoscalers"},
		{"Pod Disruption Budgets", "policy/v1/poddisruptionbudgets"},
		{"Priority Classes", "scheduling.k8s.io/v1/priorityclasses"},
	}},
	{"Network", []navItem{
		{"Services", "v1/services"},
		{"Endpoint Slices", "discovery.k8s.io/v1/endpointslices"},
		{"Ingresses", "networking.k8s.io/v1/ingresses"},
		{"Ingress Classes", "networking.k8s.io/v1/ingressclasses"},
		{"Network Policies", "networking.k8s.io/v1/networkpolicies"},
	}},
	{"Storage", []navItem{
		{"Persistent Volume Claims", "v1/persistentvolumeclaims"},
		{"Persistent Volumes", "v1/persistentvolumes"},
		{"Storage Classes", "storage.k8s.io/v1/storageclasses"},
	}},
	{"Helm", []navItem{
		{"Releases", "helm"},
	}},
	{"Access Control", []navItem{
		{"RBAC Explorer", "rbac-explorer"},
		{"Service Accounts", "v1/serviceaccounts"},
		{"Roles", "rbac.authorization.k8s.io/v1/roles"},
		{"Role Bindings", "rbac.authorization.k8s.io/v1/rolebindings"},
		{"Cluster Roles", "rbac.authorization.k8s.io/v1/clusterroles"},
		{"Cluster Role Bindings", "rbac.authorization.k8s.io/v1/clusterrolebindings"},
	}},
}

// sideRow is one flattened, clickable sidebar line.
type sideRow struct {
	header  bool
	section string
	group   bool // CRD group header
	item    navItem
	bm      *prefs.Bookmark
}

// Sidebar is the left navigation tree.
type Sidebar struct {
	app       *App
	list      widget.List
	clicks    map[string]*widget.Clickable
	collapsed map[string]bool
	crds      []navSection
	active    string
	ctxBtn    widget.Clickable
}

func newSidebar(a *App) *Sidebar {
	s := Sidebar{
		app:       a,
		clicks:    map[string]*widget.Clickable{},
		collapsed: map[string]bool{"Custom Resources": false},
	}
	s.list.Axis = layout.Vertical
	return &s
}

// reload rebuilds the CRD section from discovery.
func (s *Sidebar) reload(c *backend.Cluster) {
	groups := map[string][]navItem{}
	for _, r := range c.Resources() {
		if !r.CRD {
			continue
		}
		groups[r.Group] = append(groups[r.Group], navItem{title: r.Kind, gvr: r.GVR})
	}
	s.crds = s.crds[:0]
	for g, items := range groups {
		sort.Slice(items, func(i, j int) bool { return items[i].title < items[j].title })
		s.crds = append(s.crds, navSection{title: g, items: items})
		if _, ok := s.collapsed["crd:"+g]; !ok {
			s.collapsed["crd:"+g] = true
		}
	}
	sort.Slice(s.crds, func(i, j int) bool { return s.crds[i].title < s.crds[j].title })
}

func (s *Sidebar) rows() []sideRow {
	var rr []sideRow
	if c := s.app.cluster; c != nil {
		if bms := s.app.prefs.BookmarksFor(c.Context); len(bms) > 0 {
			rr = append(rr, sideRow{header: true, section: "Bookmarks"})
			if !s.collapsed["Bookmarks"] {
				for i := range bms {
					rr = append(rr, sideRow{section: "Bookmarks", bm: &bms[i]})
				}
			}
		}
	}
	for _, sec := range sections {
		rr = append(rr, sideRow{header: true, section: sec.title})
		if s.collapsed[sec.title] {
			continue
		}
		for _, it := range sec.items {
			rr = append(rr, sideRow{section: sec.title, item: it})
		}
	}
	rr = append(rr, sideRow{header: true, section: "Custom Resources"})
	if !s.collapsed["Custom Resources"] {
		rr = append(rr, sideRow{section: "Custom Resources", item: navItem{"Definitions", "apiextensions.k8s.io/v1/customresourcedefinitions"}})
		for _, g := range s.crds {
			rr = append(rr, sideRow{group: true, section: "crd:" + g.title, item: navItem{title: g.title}})
			if s.collapsed["crd:"+g.title] {
				continue
			}
			for _, it := range g.items {
				rr = append(rr, sideRow{section: "crd:" + g.title, item: it})
			}
		}
	}
	return rr
}

func (s *Sidebar) click(key string) *widget.Clickable {
	c, ok := s.clicks[key]
	if !ok {
		c = new(widget.Clickable)
		s.clicks[key] = c
	}
	return c
}

// Layout draws the sidebar.
func (s *Sidebar) Layout(gtx C) D {
	w := gtx.Dp(240)
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
	gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
	fill(gtx, pal.Sidebar)
	if s.ctxBtn.Clicked(gtx) {
		s.app.pickContext()
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(s.layoutCluster),
		layout.Rigid(func(gtx C) D { return hline(gtx, pal.Border) }),
		layout.Flexed(1, func(gtx C) D {
			if s.app.cluster == nil {
				return D{}
			}
			rr := s.rows()
			return layout.Inset{Top: 4, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return material.List(s.app.th, &s.list).Layout(gtx, len(rr), func(gtx C, i int) D {
					return s.layoutRow(gtx, rr[i])
				})
			})
		}),
	)
}

// layoutCluster is the Lens-like cluster card: avatar, name, state.
func (s *Sidebar) layoutCluster(gtx C) D {
	th := s.app.th
	name, state, stateC := "No cluster", "disconnected", pal.TextDim
	switch {
	case s.app.cluster != nil:
		name, state, stateC = s.app.cluster.Context, "connected · "+s.app.cluster.Version(), pal.Ok
	case s.app.connecting != "":
		name, state, stateC = s.app.connecting, "connecting…", pal.Warn
	case s.app.connErr != nil:
		state, stateC = "connection failed", pal.Err
	}
	return s.ctxBtn.Layout(gtx, func(gtx C) D {
		b := pal.Header
		if s.ctxBtn.Hovered() {
			b = pal.Hover
		}
		return bg(gtx, b, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return insetHV(12, 12).Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return avatar(gtx, th, name, 36) }),
					layout.Rigid(layout.Spacer{Width: 10}.Layout),
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 14, pal.TextBright, name) }),
							layout.Rigid(layout.Spacer{Height: 2}.Layout),
							layout.Rigid(func(gtx C) D {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx C) D { return dot(gtx, stateC, 7) }),
									layout.Rigid(layout.Spacer{Width: 5}.Layout),
									layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 11, pal.TextDim, state) }),
								)
							}),
						)
					}),
					layout.Rigid(func(gtx C) D { return mdi(gtx, icExpand, pal.TextDim, 18) }),
				)
			})
		})
	})
}

func (s *Sidebar) layoutRow(gtx C, r sideRow) D {
	th := s.app.th
	if r.bm != nil {
		return s.layoutBookmark(gtx, r.bm)
	}
	switch {
	case r.header:
		cl := s.click("sec:" + r.section)
		if cl.Clicked(gtx) {
			s.collapsed[r.section] = !s.collapsed[r.section]
		}
		return cl.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			c := pal.TextDim
			if cl.Hovered() {
				c = pal.Text
			}
			return layout.Inset{Left: 14, Right: 12, Top: 12, Bottom: 4}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						ic := sectionIcons[r.section]
						if ic == nil {
							ic = icCRD
						}
						return mdiInline(gtx, ic, c, 15)
					}),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Flexed(1, func(gtx C) D { return lbl(gtx, th, fontSans, 11, c, upper(r.section)) }),
					layout.Rigid(func(gtx C) D { return mdiInline(gtx, chevronIcon(s.collapsed[r.section]), c, 14) }),
				)
			})
		})
	case r.group:
		cl := s.click(r.section)
		if cl.Clicked(gtx) {
			s.collapsed[r.section] = !s.collapsed[r.section]
		}
		return cl.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			c := pal.TextDim
			if cl.Hovered() {
				c = pal.Text
			}
			return layout.Inset{Left: 32, Right: 12, Top: 5, Bottom: 5}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return mdiInline(gtx, chevronIcon(s.collapsed[r.section]), c, 13) }),
					layout.Rigid(layout.Spacer{Width: 4}.Layout),
					layout.Flexed(1, func(gtx C) D { return lbl(gtx, th, fontSans, 12, c, r.item.title) }),
				)
			})
		})
	}
	cl := s.click("gvr:" + r.item.gvr)
	if cl.Clicked(gtx) {
		s.app.openResource(r.item.gvr)
	}
	left := unit.Dp(30)
	crd := len(r.section) > 4 && r.section[:4] == "crd:"
	if crd {
		left = 50
	}
	return cl.Layout(gtx, func(gtx C) D {
		active := s.active == r.item.gvr
		h := gtx.Dp(28)
		sz := image.Pt(gtx.Constraints.Max.X, h)
		m := gtx.Dp(6)
		pillR := image.Rect(m, gtx.Dp(1), sz.X-m, h-gtx.Dp(1))
		switch {
		case active:
			fillRRect(gtx, pal.Selected, pillR, gtx.Dp(5))
			fillRRect(gtx, pal.Accent, image.Rect(m, gtx.Dp(6), m+gtx.Dp(3), h-gtx.Dp(6)), gtx.Dp(2))
		case cl.Hovered():
			fillRRect(gtx, pal.Hover, pillR, gtx.Dp(5))
		}
		gtx.Constraints = layout.Exact(sz)
		return layout.Inset{Left: left, Right: 8}.Layout(gtx, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D {
				c, ic := pal.Text, pal.TextDim
				if active {
					c, ic = pal.TextBright, pal.Accent
				}
				if crd {
					return lbl(gtx, th, fontSans, 13, c, r.item.title)
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return mdiInline(gtx, iconFor(r.item.gvr), ic, 15) }),
					layout.Rigid(layout.Spacer{Width: 9}.Layout),
					layout.Flexed(1, func(gtx C) D { return lbl(gtx, th, fontSans, 13, c, r.item.title) }),
				)
			})
		})
	})
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func chevronIcon(collapsed bool) *widget.Icon {
	if collapsed {
		return icCollapse
	}
	return icExpand
}

func (s *Sidebar) layoutBookmark(gtx C, b *prefs.Bookmark) D {
	th := s.app.th
	bm := *b
	cl := s.click("bm:" + b.Name)
	if cl.Clicked(gtx) {
		s.app.openBookmark(bm)
	}
	return cl.Layout(gtx, func(gtx C) D {
		active := s.active == "bm:"+b.Name
		h := gtx.Dp(28)
		sz := image.Pt(gtx.Constraints.Max.X, h)
		m := gtx.Dp(6)
		pillR := image.Rect(m, gtx.Dp(1), sz.X-m, h-gtx.Dp(1))
		switch {
		case active:
			fillRRect(gtx, pal.Selected, pillR, gtx.Dp(5))
		case cl.Hovered():
			fillRRect(gtx, pal.Hover, pillR, gtx.Dp(5))
		}
		gtx.Constraints = layout.Exact(sz)
		return layout.Inset{Left: 30, Right: 8}.Layout(gtx, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D {
				c := pal.Text
				if active {
					c = pal.TextBright
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return mdi(gtx, icBookmark, pal.Accent, 15) }),
					layout.Rigid(layout.Spacer{Width: 9}.Layout),
					layout.Flexed(1, func(gtx C) D { return lbl(gtx, th, fontSans, 13, c, bm.Name) }),
				)
			})
		})
	})
}
