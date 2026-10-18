//go:build dev && integration

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/twoojoo/kubeon/internal/ai"
	"github.com/twoojoo/kubeon/internal/backend"
)

// TestUISiteScreenshots renders the screenshots of the website
// (internal/site/static/img) against the kind cluster. It runs only with
// KUBEON_SITE_SHOTS=<dir> (make site-shots).
func TestUISiteScreenshots(t *testing.T) {
	out := os.Getenv("KUBEON_SITE_SHOTS")
	if out == "" {
		t.Skip("KUBEON_SITE_SHOTS not set (make site-shots)")
	}
	out, _ = filepath.Abs(out)
	isolatedPrefs(t)
	// the assistant panel needs a provider; nothing is sent to it
	cfg := ai.LoadConfig()
	cfg.Set("ollama", ai.ProviderConfig{Model: "llama3.1:8b"})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, Options{Namespace: "demo", Resource: "po"})
	h.dir = out
	a := h.a
	selectRow(t, h, "demo/web-")
	h.settle(500 * time.Millisecond)
	h.shot("pods")

	// light theme on the same table
	a.toggleTheme()
	h.settle(300 * time.Millisecond)
	h.shot("light")
	a.toggleTheme()

	// protected context: typed confirmation of a delete
	ctx := a.cluster.Context
	a.prefs.SetPolicy(ctx, true, false)
	h.frame()
	a.view.confirmDelete()
	if _, ok := a.overlay.(*Confirm); !ok {
		t.Fatalf("no confirmation: %T", a.overlay)
	}
	h.typeText("web")
	h.settle(300 * time.Millisecond)
	h.shot("protected")
	h.press(key.NameEscape)
	a.overlay = nil
	a.prefs.SetPolicy(ctx, false, false)

	// shell in a web pod
	h.press("S")
	h.waitFor("shell picker", func() bool { _, ok := a.overlay.(*Picker); return ok })
	h.press(key.NameReturn)
	h.waitFor("shell", func() bool {
		tm, ok := a.dock.tabs[len(a.dock.tabs)-1].(*Terminal)
		return ok && tm.sess != nil
	})
	h.settle(time.Second)
	h.typeText("hostname; ls /; ps")
	h.press(key.NameReturn)
	h.settle(1500 * time.Millisecond)
	h.shot("shell")
	a.dock.closeAll()

	// multi-pod JSON logs of a deployment
	a.openResource("apps/v1/deployments")
	selectRow(t, h, "demo/jsonlog")
	h.press("L")
	h.waitFor("json logs", func() bool {
		if len(a.dock.tabs) == 0 {
			return false
		}
		lv, _ := a.dock.tabs[len(a.dock.tabs)-1].(*LogView)
		return lv != nil && lv.count() > 30 && lv.multiPod
	})
	h.settle(500 * time.Millisecond)
	h.shot("logs")
	a.dock.closeAll()

	// port-forward manager with a pinned tunnel
	selectRow(t, h, "demo/web")
	pf, err := a.cluster.PortForward("apps/v1/deployments", "demo/web", 0, 8080)
	if err != nil {
		t.Fatal(err)
	}
	panel := newPortForwardPanel(a)
	a.overlay = panel
	h.settle(100 * time.Millisecond)
	row := panel.rows[0]
	h.gtxForTest().Execute(key.FocusCmd{Tag: &row.name})
	h.frame()
	h.typeText("web (demo)")
	row.pin.Click()
	h.settle(300 * time.Millisecond)
	h.shot("portforwards")
	a.cluster.StopPortForward(pf.ID)
	a.overlay = nil

	// XRay graph of a deployment
	h.press("X")
	h.waitFor("xray", func() bool { return a.page == xrayPage && a.xray.root != nil && a.xray.root.Count() > 5 })
	a.xray.mode = xrayGraph
	h.settle(300 * time.Millisecond)
	h.shot("xray")
	h.press(key.NameEscape)

	// assistant with a proposal waiting for approval
	three := int32(3)
	h.press("A")
	a.ai.add(&chatItem{kind: chatUser, text: "Is web healthy? Make sure it keeps 3 replicas."})
	a.ai.add(&chatItem{kind: chatTool, tool: &ai.ToolRun{ID: "t1", Name: "describe_resource", Args: "kind=deploy name=web", Output: "Replicas: 3 desired | 3 available", Duration: 420 * time.Millisecond}})
	a.ai.add(&chatItem{kind: chatAssistant, text: "The deployment **web** runs `3/3` replicas and its rollout is complete.\n\n- no warning events\n- readiness probes pass\n\nI can pin the replica count:"})
	a.ai.add(&chatItem{kind: chatProposal, cluster: a.cluster, prop: &ai.Proposal{Action: "scale", Kind: "deploy", GVR: "apps/v1/deployments", Name: "web", Namespace: "demo", Replicas: &three, Reason: "Keep 3 replicas for availability."}})
	h.settle(300 * time.Millisecond)
	h.shot("assistant")
	a.ai.open = false

	// YAML edit reviewed as a diff (not applied)
	a.openResource("v1/configmaps")
	selectRow(t, h, "demo/settings")
	a.view.details(tabYAML)
	h.waitFor("yaml", func() bool { return a.drawer.open && len(a.drawer.yamlV.lines) > 5 })
	a.view.edit()
	h.frame()
	a.drawer.yaml.SetText(strings.Replace(a.drawer.yaml.Text(), "level: debug", "level: info", 1))
	h.frame()
	h.frame()
	a.drawer.saveBtn.Click()
	h.frame()
	r, ok := a.overlay.(*DiffReview)
	if !ok {
		t.Fatalf("expected diff review, got %T", a.overlay)
	}
	h.waitFor("dry run", func() bool { return r.dry != "running" })
	h.settle(300 * time.Millisecond)
	h.shot("diff")
	a.overlay = nil
	a.drawer.setEditing(false)
	a.drawer.dirty = false // discard the edit without a "kept" message
	a.drawer.close()

	// helm values
	installChart(t, a.opts.Kubeconfig)
	a.openResource(backend.HelmGVR)
	selectRow(t, h, "demo/ui-rel")
	h.press("V")
	h.waitFor("values", func() bool { return a.drawer.open && strings.Contains(a.drawer.values.Text(), "hola") })
	h.settle(300 * time.Millisecond)
	h.shot("helm")
	h.press(key.NameEscape)

	// sanitizer
	a.openResource(sanitizerPage)
	h.waitFor("report", func() bool { return a.san.report != nil })
	h.settle(300 * time.Millisecond)
	h.shot("sanitizer")

	// RBAC explorer: rules of the scheduler
	a.openResource(rbacPage)
	h.waitFor("rbac index", func() bool { return a.rbac.idx != nil })
	for i, s := range a.rbac.idx.Subjects {
		if s.Kind == "User" && s.Name == "system:kube-scheduler" {
			a.rbac.sel = i
			a.rbac.refreshRules()
		}
	}
	a.rbac.verb.SetText("get")
	a.rbac.res.SetText("pods")
	a.rbac.ns.SetText("demo")
	a.rbac.check()
	h.waitFor("sar", func() bool { return a.rbac.sar != "…" })
	h.settle(300 * time.Millisecond)
	h.shot("rbac")

	// overview last: its warning list follows the crasher pod
	a.openResource(overviewGVR)
	h.waitFor("overview", func() bool { return a.ovw.data != nil && len(a.ovw.data.Warnings) > 0 })
	h.settle(500 * time.Millisecond)
	h.shot("overview")
}
