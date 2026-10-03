package ui

import (
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
	{"Access Control", []navItem{
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
	w := gtx.Dp(230)
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
	gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
	fill(gtx, pal.Sidebar)
	th := s.app.th
	if s.ctxBtn.Clicked(gtx) {
		s.app.pickContext()
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return s.ctxBtn.Layout(gtx, func(gtx C) D {
				return bg(gtx, pal.Header, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return insetHV(14, 12).Layout(gtx, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return labelIcon(gtx, th, 11, pal.TextDim, "CLUSTER", iconChevronDown)
							}),
							layout.Rigid(func(gtx C) D {
								name := "—"
								switch {
								case s.app.cluster != nil:
									name = s.app.cluster.Context
								case s.app.connecting != "":
									name = s.app.connecting
								}
								return lbl(gtx, th, fontSans, 15, pal.TextBright, name)
							}),
						)
					})
				})
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			if s.app.cluster == nil {
				return D{}
			}
			rr := s.rows()
			return material.List(th, &s.list).Layout(gtx, len(rr), func(gtx C, i int) D {
				return s.layoutRow(gtx, rr[i])
			})
		}),
	)
}

func (s *Sidebar) layoutRow(gtx C, r sideRow) D {
	th := s.app.th
	switch {
	case r.header:
		cl := s.click("sec:" + r.section)
		if cl.Clicked(gtx) {
			s.collapsed[r.section] = !s.collapsed[r.section]
		}
		return cl.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Inset{Left: 14, Right: 10, Top: 12, Bottom: 4}.Layout(gtx, func(gtx C) D {
				return iconLabel(gtx, th, 11, pal.TextDim, chevron(s.collapsed[r.section]), upper(r.section))
			})
		})
	case r.group:
		cl := s.click(r.section)
		if cl.Clicked(gtx) {
			s.collapsed[r.section] = !s.collapsed[r.section]
		}
		return cl.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Inset{Left: 22, Right: 10, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
				return iconLabel(gtx, th, 12, pal.TextDim, chevron(s.collapsed[r.section]), r.item.title)
			})
		})
	}
	cl := s.click("gvr:" + r.item.gvr)
	if cl.Clicked(gtx) {
		s.app.openResource(r.item.gvr)
	}
	left := unit.Dp(26)
	if len(r.section) > 4 && r.section[:4] == "crd:" {
		left = 38
	}
	return cl.Layout(gtx, func(gtx C) D {
		active := s.active == r.item.gvr
		h := gtx.Dp(26)
		sz := image.Pt(gtx.Constraints.Max.X, h)
		switch {
		case active:
			fillRect(gtx, pal.Selected, image.Rectangle{Max: sz})
			fillRect(gtx, pal.Accent, image.Rect(0, 0, gtx.Dp(3), h))
		case cl.Hovered():
			fillRect(gtx, pal.Hover, image.Rectangle{Max: sz})
		}
		gtx.Constraints = layout.Exact(sz)
		return layout.Inset{Left: left, Right: 8}.Layout(gtx, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D {
				c := pal.Text
				if active {
					c = pal.TextBright
				}
				return lbl(gtx, th, fontSans, 13, c, r.item.title)
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

func chevron(collapsed bool) iconKind {
	if collapsed {
		return iconChevronRight
	}
	return iconChevronDown
}
