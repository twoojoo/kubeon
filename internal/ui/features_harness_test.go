//go:build dev && integration

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/twoojoo/kubeon/internal/backend"
)

func selectRow(t *testing.T, h *harness, prefix string) {
	t.Helper()
	h.waitFor("row "+prefix, func() bool {
		s := h.a.view.snap
		if s == nil {
			return false
		}
		for i, r := range s.Rows {
			if strings.HasPrefix(r.ID, prefix) {
				h.a.view.setSel(i)
				return true
			}
		}
		return false
	})
}

func installChart(t *testing.T, kc string) {
	t.Helper()
	ns := "demo"
	flags := &genericclioptions.ConfigFlags{KubeConfig: &kc, Namespace: &ns}
	cfg := new(action.Configuration)
	if err := cfg.Init(flags, ns, "secret", func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	ch, err := loader.Load(filepath.Join("..", "..", "testdata", "charts", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = action.NewUninstall(cfg).Run("ui-rel")
	in := action.NewInstall(cfg)
	in.ReleaseName, in.Namespace = "ui-rel", ns
	if _, err := in.Run(ch, map[string]any{"greeting": "ciao"}); err != nil {
		t.Fatal(err)
	}
	up := action.NewUpgrade(cfg)
	up.Namespace = ns
	if _, err := up.Run("ui-rel", ch, map[string]any{"greeting": "hola"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = action.NewUninstall(cfg).Run("ui-rel") })
}

func TestUIFeatures(t *testing.T) {
	dl := filepath.Join("..", "..", ".dev", "downloads")
	_ = os.RemoveAll(dl)
	t.Setenv("KUBEON_DOWNLOAD_DIR", dl)
	h := newHarness(t, Options{Namespace: "demo", Resource: "secrets"})
	a := h.a

	// --- secrets: decode tab, reveal, download
	selectRow(t, h, "demo/db-creds")
	h.press("X")
	h.waitFor("secret data", func() bool { return a.drawer.open && len(a.drawer.secret) == 3 })
	h.shot("20-secret-masked")
	a.drawer.revealAll = true
	h.settle(100 * time.Millisecond)
	h.shot("21-secret-revealed")
	a.drawer.downloadSecret()
	b, err := os.ReadFile(filepath.Join(dl, "demo_db-creds.env"))
	if err != nil || !strings.Contains(string(b), "PASSWORD=s3cr3t!") {
		t.Fatalf("secret env: %q %v", b, err)
	}
	h.press(key.NameEscape)

	// --- multi-pod JSON logs
	a.openResource("apps/v1/deployments")
	selectRow(t, h, "demo/jsonlog")
	h.press("L")
	var lv *LogView
	h.waitFor("json logs", func() bool {
		if len(a.dock.tabs) == 0 {
			return false
		}
		lv, _ = a.dock.tabs[len(a.dock.tabs)-1].(*LogView)
		return lv != nil && lv.count() > 10 && lv.multiPod
	})
	h.shot("22-logs-json-multipod")
	lv.minLevel = lvlError
	lv.rebuildFilter()
	for i := 0; i < lv.count(); i++ {
		if lv.at(i).level != lvlError {
			t.Fatalf("level filter leak: %+v", lv.at(i))
		}
	}
	h.settle(100 * time.Millisecond)
	h.shot("23-logs-errors-only")
	lv.download()
	ents, _ := os.ReadDir(dl)
	foundLog := false
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".log") {
			foundLog = true
		}
	}
	if !foundLog {
		t.Fatal("log not downloaded")
	}
	a.dock.closeAll()

	// --- xray tree + graph
	selectRow(t, h, "demo/web")
	h.press("X")
	h.waitFor("xray", func() bool { return a.page == xrayPage && a.xray.root != nil && a.xray.root.Count() > 5 })
	h.shot("24-xray-tree")
	a.xray.mode = xrayGraph
	h.settle(150 * time.Millisecond)
	h.shot("25-xray-graph")
	a.xray.focus = ""
	a.xray.rebuild()
	h.settle(150 * time.Millisecond)
	h.shot("26-xray-graph-all")
	h.press(key.NameEscape) // back to deployments
	if a.page == xrayPage {
		t.Fatal("esc should go back")
	}

	// --- nodes: cordon via UI
	a.openResource("v1/nodes")
	selectRow(t, h, "kubeon-dev")
	h.press("C")
	h.waitFor("cordon confirm", func() bool { _, ok := a.overlay.(*Confirm); return ok })
	h.shot("27-cordon-confirm")
	h.press(key.NameReturn)
	h.waitFor("cordoned", func() bool {
		r, ok := a.view.selected()
		return ok && nodeCordoned(r)
	})
	h.shot("28-node-cordoned")
	h.press("R")
	h.waitFor("drain confirm", func() bool { _, ok := a.overlay.(*Confirm); return ok })
	h.shot("29-drain-confirm")
	h.press(key.NameEscape)
	h.press("U")
	h.waitFor("uncordon confirm", func() bool { _, ok := a.overlay.(*Confirm); return ok })
	h.press(key.NameReturn)
	h.waitFor("uncordoned", func() bool {
		r, ok := a.view.selected()
		return ok && !nodeCordoned(r)
	})

	// --- helm
	installChart(t, a.opts.Kubeconfig)
	a.openResource(backend.HelmGVR)
	selectRow(t, h, "demo/ui-rel")
	h.shot("30-helm-releases")
	h.press("V")
	h.waitFor("values", func() bool { return a.drawer.open && strings.Contains(a.drawer.values.Text(), "hola") })
	h.shot("31-helm-values")
	h.press(key.NameEscape)
	h.press(key.NameReturn)
	h.waitFor("history", func() bool {
		return a.view.gvr == backend.HelmHistoryGVR && a.view.snap != nil && len(a.view.snap.Rows) == 2
	})
	h.shot("32-helm-history")

	// --- notifications panel with synthetic events
	a.onHealth([]backend.HealthEvent{
		{Severity: backend.SevErr, Title: "Pod CrashLoopBackOff", Body: "crasher in namespace demo", GVR: "v1/pods", Path: "demo/crasher", At: time.Now().Add(-2 * time.Minute)},
		{Severity: backend.SevWarn, Title: "Deployment degraded", Body: "demo/web: 2/3 available", GVR: "apps/v1/deployments", Path: "demo/web", At: time.Now().Add(-time.Minute)},
		{Severity: backend.SevOK, Title: "Rollout complete", Body: "demo/web: 3/3 available", GVR: "apps/v1/deployments", Path: "demo/web", At: time.Now()},
	})
	h.settle(100 * time.Millisecond)
	h.shot("33-bell-unread")
	a.overlay = newNotifPanel(a)
	h.settle(100 * time.Millisecond)
	h.shot("34-notifications")
}
