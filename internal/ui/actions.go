package ui

import (
	"fmt"
	"strconv"
	"strings"

	"gioui.org/io/system"

	"github.com/giovanni/kubeon/internal/backend"
)

// enter drills down like k9s: namespaces → pods, CRDs → instances,
// otherwise opens the details drawer.
func (v *ResourceView) enter() {
	r, ok := v.selected()
	if !ok {
		return
	}
	switch v.gvr {
	case backend.HelmGVR:
		v.helmHistory()
		return
	case "v1/namespaces":
		v.app.ns = r.Cells[0]
		v.app.openResource("v1/pods")
		return
	case "apiextensions.k8s.io/v1/customresourcedefinitions":
		for _, res := range v.app.cluster.Resources() {
			if res.Name+"."+res.Group == r.Cells[0] {
				v.app.openResource(res.GVR)
				return
			}
		}
	}
	v.details(tabOverview)
}

func (v *ResourceView) details(tab int) {
	r, ok := v.selected()
	if !ok {
		return
	}
	v.app.drawer.show(v.gvr, r.ID, tab, false)
}

func (v *ResourceView) edit() {
	r, ok := v.selected()
	if !ok {
		return
	}
	v.app.drawer.show(v.gvr, r.ID, tabYAML, true)
}

func (v *ResourceView) logs(previous bool) {
	r, ok := v.selected()
	c := v.app.cluster
	if !ok || !c.IsLoggable(v.gvr) {
		return
	}
	v.app.dock.openLogs(v.gvr, r.ID, previous)
}

func (v *ResourceView) shell() {
	r, ok := v.selected()
	if !ok || v.gvr != "v1/pods" {
		return
	}
	a := v.app
	if !a.allowMutation("Shell") {
		return
	}
	c := a.cluster
	path := r.ID
	go func() {
		cc, err := c.Containers(path)
		a.post(func() {
			if a.stale(c) {
				return
			}
			if err != nil {
				a.flash("containers: "+err.Error(), true)
				return
			}
			open := func(co string) {
				if a.stale(c) {
					return
				}
				if a.protected() {
					a.overlay = a.guard(newConfirm(a, "Open shell", fmt.Sprintf("Open an interactive shell in %s/%s?", path, co), true,
						func(bool) {
							if !a.stale(c) {
								a.dock.openShell(path, co)
							}
						}), path)
					return
				}
				a.dock.openShell(path, co)
			}
			if len(cc) == 1 {
				open(cc[0])
				return
			}
			p := newPicker(a, "Shell into container", cc, false, open)
			p.icon = icTerminal
			a.overlay = p
		})
	}()
}

func (v *ResourceView) scale() {
	r, ok := v.selected()
	c := v.app.cluster
	if !ok || !c.CanScale(v.gvr) {
		return
	}
	a, gvr, path := v.app, v.gvr, r.ID
	if !a.allowMutation("Scale") {
		return
	}
	go func() {
		cur, err := c.Replicas(gvr, path)
		a.post(func() {
			if a.stale(c) {
				return
			}
			if err != nil {
				a.flash("replicas: "+err.Error(), true)
				return
			}
			p := newPicker(a, fmt.Sprintf("Scale %s (current %d) — type replicas", path, cur), nil, true, func(s string) {
				if a.stale(c) {
					return
				}
				n, err := strconv.Atoi(strings.TrimSpace(s))
				if err != nil || n < 0 {
					a.flash("invalid replica count", true)
					return
				}
				run := func() {
					if a.stale(c) {
						return
					}
					a.do(fmt.Sprintf("Scale %s to %d", path, n), func() error {
						return c.Scale(gvr, path, int32(n))
					})
				}
				if a.protected() {
					a.overlay = a.guard(newConfirm(a, "Scale", fmt.Sprintf("Scale %s from %d to %d replicas?", path, cur, n), true, func(bool) { run() }), path)
					return
				}
				run()
			})
			p.editor.SetText(strconv.Itoa(int(cur)))
			a.overlay = p
		})
	}()
}

func (v *ResourceView) restart() {
	if r, ok := v.selected(); ok {
		v.restartID(r.ID)
	}
}

func (v *ResourceView) restartID(path string) {
	c := v.app.cluster
	if !c.CanRestart(v.gvr) {
		return
	}
	a, gvr := v.app, v.gvr
	a.confirmMutation("Restart", fmt.Sprintf("Rollout restart %s?", path), path, false, func() {
		a.do("Restart "+path, func() error { return c.Restart(gvr, path) })
	})
}

func (v *ResourceView) confirmDelete() {
	if r, ok := v.selected(); ok {
		v.confirmDeleteID(r.ID)
	}
}

func (v *ResourceView) confirmDeleteID(id string) {
	if v.gvr == backend.HelmGVR {
		a, c, rel := v.app, v.app.cluster, id
		a.confirmMutation("Uninstall", fmt.Sprintf("Uninstall helm release %s?", rel), rel, true, func() {
			a.do("Uninstall "+rel, func() error { return c.HelmUninstall(rel) })
		})
		return
	}
	if isHelm(v.gvr) || !backend.CanVerb(v.gvr, "delete") {
		return
	}
	a, gvr, path, c := v.app, v.gvr, id, v.app.cluster
	if !a.allowMutation("Delete") {
		return
	}
	a.overlay = a.guard(newConfirm(a, "Delete", fmt.Sprintf("Delete %s %s?", titleFor(gvr), path), true, func(force bool) {
		a.do("Delete "+path, func() error { return c.Delete(gvr, path, force) })
	}), path)
}

func (a *App) pickNamespace() {
	if a.cluster == nil {
		return
	}
	a.refreshNamespaces()
	items := append([]string{"All namespaces"}, a.namespaces...)
	p := newPicker(a, "Namespace", items, false, func(s string) {
		if s == "All namespaces" {
			s = ""
		}
		a.setNamespace(s)
	})
	p.icon = icNamespace
	a.overlay = p
}

func (a *App) pickContext() {
	p := newPicker(a, "Switch context", a.contexts, false, func(s string) {
		a.connect(s)
	})
	p.icon = icCluster
	a.overlay = p
}

// openPalette opens the k9s-like command prompt: resource aliases
// (po, deploy, svc, crd names…), "ns <name>", "ctx <name>".
func (a *App) openPalette(prefill string) {
	if a.cluster == nil {
		return
	}
	var items []string
	seen := map[string]bool{}
	for _, sec := range sections {
		for _, it := range sec.items {
			items = append(items, it.title+"  ·  "+it.gvr)
			seen[it.gvr] = true
		}
	}
	for _, r := range a.cluster.Resources() {
		if !seen[r.GVR] {
			items = append(items, r.Kind+"  ·  "+r.GVR)
		}
	}
	for _, n := range a.namespaces {
		items = append(items, "ns "+n)
	}
	for _, n := range a.contexts {
		items = append(items, "ctx "+n)
	}
	p := newPicker(a, "Command  (resource alias, ns <name>, ctx <name>)", items, true, a.runCommand)
	p.exact = func(s string) bool {
		f := strings.Fields(strings.TrimPrefix(s, ":"))
		if len(f) == 0 {
			return false
		}
		switch f[0] {
		case "ns", "namespace", "ctx", "context", "q", "quit", "pf", "portforwards", "bm", "bookmarks", "bookmark", "theme", "settings", "prefs", "preferences", "tab", "notify", "notifications", "events", "ai", "assistant", "chat", "xray", "helm", "charts", "releases":
			return true
		}
		_, ok := backend.LookupAlias(f[0])
		return ok
	}
	p.editor.SetText(prefill)
	a.overlay = p
}

func (a *App) runCommand(cmd string) {
	cmd = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cmd), ":"))
	if i := strings.Index(cmd, "  ·  "); i >= 0 {
		a.openResource(cmd[i+len("  ·  "):])
		return
	}
	f := strings.Fields(cmd)
	if len(f) == 0 {
		return
	}
	switch f[0] {
	case "ns", "namespace":
		if len(f) == 1 {
			a.pickNamespace()
			return
		}
		a.setNamespace(nsArg(f[1]))
		return
	case "ctx", "context":
		if len(f) == 1 {
			a.pickContext()
			return
		}
		a.connect(f[1])
		return
	case "pf", "portforwards":
		a.overlay = newPortForwardPanel(a)
		return
	case "bm", "bookmarks":
		a.pickBookmark()
		return
	case "bookmark":
		a.bookmarkCurrent()
		return
	case "theme":
		want := "light"
		if len(f) > 1 {
			want = f[1]
		} else if a.prefs.Theme == "light" {
			want = "dark"
		}
		if want != a.prefs.Theme {
			a.toggleTheme()
		}
		return
	case "settings", "prefs", "preferences":
		a.overlay = newPrefsPanel(a)
		return
	case "tab":
		a.newTab()
		return
	case "ai", "assistant", "chat":
		a.ai.open = true
		if len(f) > 1 {
			a.ai.send(strings.Join(f[1:], " "))
		}
		return
	case "notify", "notifications", "events":
		a.overlay = newNotifPanel(a)
		return
	case "xray":
		gvr := a.view.gvr
		if len(f) > 1 {
			if g, ok := backend.LookupAlias(f[1]); ok {
				gvr = g
			}
		}
		if !backend.CanXRay(gvr) {
			a.flash("xray supports deploy, sts, ds, rs, svc, pods", true)
			return
		}
		a.drill(navState{page: xrayPage, gvr: gvr})
		return
	case "helm", "charts", "releases":
		a.openResource(backend.HelmGVR)
		return
	case "q", "quit":
		if a.w != nil {
			a.w.Perform(system.ActionClose)
		}
		return
	}
	gvr, ok := backend.LookupAlias(f[0])
	if !ok {
		a.flash(fmt.Sprintf("unknown command %q", f[0]), true)
		return
	}
	if len(f) > 1 {
		a.ns = nsArg(f[1])
	}
	a.openResource(gvr)
}

func canForward(gvr string) bool {
	switch gvr {
	case "v1/pods", "v1/services", "apps/v1/deployments", "apps/v1/statefulsets", "apps/v1/daemonsets":
		return true
	}
	return false
}

// stale reports (and tells the user) that the context changed since an
// action on cluster c started: its continuation must not run on another
// cluster or under another cluster's policy.
func (a *App) stale(c *backend.Cluster) bool {
	if a.cluster == c {
		return false
	}
	a.flash("Context changed: action cancelled", true)
	return true
}

// portForward asks for "local:remote" and opens a tunnel.
func (v *ResourceView) portForward() {
	r, ok := v.selected()
	if !ok || !canForward(v.gvr) {
		return
	}
	a, c, gvr, path := v.app, v.app.cluster, v.gvr, r.ID
	go func() {
		ports, err := c.ContainerPorts(gvr, path)
		a.post(func() {
			if a.stale(c) {
				return
			}
			if err != nil {
				a.flash("ports: "+err.Error(), true)
				return
			}
			var items []string
			for _, p := range ports {
				items = append(items, fmt.Sprintf("%d:%d", p, p))
			}
			p := newPicker(a, "Port-forward "+path+"  (local:remote, local 0 = random)", items, true, func(s string) {
				if a.stale(c) {
					return
				}
				lp, rp, ok := strings.Cut(strings.TrimSpace(s), ":")
				if !ok {
					lp, rp = "0", lp
				}
				l, e1 := strconv.Atoi(lp)
				rr, e2 := strconv.Atoi(rp)
				if e1 != nil || e2 != nil || rr <= 0 {
					a.flash("invalid ports, use local:remote", true)
					return
				}
				a.confirmForward(path, rr, func() {
					if a.stale(c) {
						return
					}
					a.flash("Port-forward "+path+"…", false)
					go func() {
						pf, err := c.PortForward(gvr, path, l, rr)
						a.post(func() {
							if err != nil {
								a.flash("port-forward failed: "+err.Error(), true)
								return
							}
							a.flash("Forwarding "+a.forwardTitle(forwardRef(c.Context, gvr, path, pf.Local, rr), pf.String()), false)
						})
					}()
				})
			})
			p.exact = func(s string) bool { return strings.Contains(s, ":") }
			a.overlay = p
		})
	}()
}

// pickForward lists active forwards; picking one stops it.
func (a *App) pickForward() {
	if a.cluster == nil {
		return
	}
	pfs := a.cluster.PortForwards()
	if len(pfs) == 0 {
		a.flash("no active port-forwards", false)
		return
	}
	var items []string
	ids := map[string]string{}
	for _, p := range pfs {
		item := a.forwardTitle(forwardRef(a.cluster.Context, p.GVR, p.Target, p.Local, p.Remote), p.String())
		items = append(items, item)
		ids[item] = p.ID
	}
	c := a.cluster
	a.overlay = newPicker(a, "Active port-forwards — select to stop", items, false, func(s string) {
		c.StopPortForward(ids[s])
		a.flash("Stopped "+s, false)
	})
}

func nodeCordoned(r backend.Row) bool {
	for _, c := range r.Cells {
		if strings.Contains(c, "SchedulingDisabled") {
			return true
		}
	}
	return false
}

func (v *ResourceView) toggleCordon() {
	r, ok := v.selected()
	if !ok || v.gvr != "v1/nodes" {
		return
	}
	a, c, node := v.app, v.app.cluster, r.ID
	on := !nodeCordoned(r)
	verb := "Cordon"
	if !on {
		verb = "Uncordon"
	}
	a.confirmMutation(verb, fmt.Sprintf("%s node %s?", verb, node), node, on, func() {
		a.do(verb+" "+node, func() error { return c.Cordon(node, on) })
	})
}

func (v *ResourceView) drain() {
	r, ok := v.selected()
	if !ok || v.gvr != "v1/nodes" {
		return
	}
	a, c, node := v.app, v.app.cluster, r.ID
	if !a.allowMutation("Drain") {
		return
	}
	cf := newConfirmOpts(a, "Drain", fmt.Sprintf("Cordon %s and evict all its pods?", node), true,
		[]string{"Ignore DaemonSets", "Delete emptyDir data", "Force (unmanaged pods)"}, []bool{true, false, false},
		func(o []bool) {
			a.do("Drain "+node, func() error {
				out, err := c.Drain(node, backend.DrainOpts{IgnoreDaemonSets: o[0], DeleteEmptyDirData: o[1], Force: o[2], GracePeriodSeconds: -1})
				if err != nil && out != "" {
					return fmt.Errorf("%w: %s", err, lastLine(out))
				}
				return err
			})
		})
	a.overlay = a.guard(cf, node)
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func (v *ResourceView) xray() {
	r, ok := v.selected()
	if !backend.CanXRay(v.gvr) {
		return
	}
	focus := ""
	if ok {
		focus = r.ID
	}
	v.app.drill(navState{page: xrayPage, gvr: v.gvr, path: focus})
}

func (v *ResourceView) helmHistory() {
	r, ok := v.selected()
	if !ok || v.gvr != backend.HelmGVR {
		return
	}
	v.app.drill(navState{page: backend.HelmHistoryGVR, gvr: backend.HelmHistoryGVR, path: r.ID})
}

func (v *ResourceView) rollback() {
	r, ok := v.selected()
	if !ok || v.gvr != backend.HelmHistoryGVR {
		return
	}
	a, c := v.app, v.app.cluster
	rel, rev := backend.SplitHistoryID(r.ID)
	a.confirmMutation("Rollback", fmt.Sprintf("Roll back %s to revision %s?", rel, rev), rel, false, func() {
		a.do("Rollback "+rel+" to "+rev, func() error { return c.HelmRollback(rel, rev) })
	})
}

// nsArg maps a palette namespace argument: "all" means all namespaces;
// anything else is a namespace name, "alloy" included.
func nsArg(s string) string {
	if s == "all" {
		return ""
	}
	return s
}
