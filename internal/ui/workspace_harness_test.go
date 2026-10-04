//go:build dev && integration

package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/giovanni/k9desk/internal/prefs"
)

func isolatedPrefs(t *testing.T) string {
	dir, _ := filepath.Abs(filepath.Join("..", "..", ".dev", "prefs-test"))
	_ = os.RemoveAll(dir)
	t.Setenv("K9DESK_CONFIG_DIR", dir)
	return dir
}

func TestUIThemeAndSafety(t *testing.T) {
	isolatedPrefs(t)
	h := newHarness(t, Options{Namespace: "demo", Resource: "po"})
	a := h.a
	selectRow(t, h, "demo/web")

	// light theme
	a.toggleTheme()
	h.settle(150 * time.Millisecond)
	h.shot("50-light-pods")
	a.view.details(tabDescribe)
	h.waitFor("drawer", func() bool { return a.drawer.open && !a.drawer.loading })
	h.shot("51-light-describe")
	h.press(key.NameEscape)
	if prefs.Load().Theme != "light" {
		t.Fatal("theme not persisted")
	}
	a.toggleTheme()

	// protected context: type-to-confirm
	ctx := a.cluster.Context
	a.prefs.SetPolicy(ctx, true, false)
	h.frame()
	r, _ := a.view.selected()
	a.view.confirmDelete()
	cf, ok := a.overlay.(*Confirm)
	if !ok || cf.typeName != shortName(r.ID) {
		t.Fatalf("expected type-to-confirm dialog, got %#v", a.overlay)
	}
	h.press(key.NameReturn)
	if cf.done {
		t.Fatal("confirmed without typing the name")
	}
	h.typeText("wrong")
	h.settle(80 * time.Millisecond)
	h.shot("52-protected-confirm")
	if cf.armed() {
		t.Fatal("armed with wrong name")
	}
	h.press(key.NameEscape)

	// read-only: mutations blocked
	a.prefs.SetPolicy(ctx, true, true)
	h.settle(80 * time.Millisecond)
	h.shot("53-protected-readonly")
	a.view.confirmDelete()
	if a.overlay != nil {
		t.Fatal("read-only must block delete")
	}
	if !strings.Contains(a.status, "read-only") {
		t.Fatalf("status %q", a.status)
	}
	a.prefs.SetPolicy(ctx, false, false)
}

func TestUISelectionAndMenus(t *testing.T) {
	isolatedPrefs(t)
	h := newHarness(t, Options{Namespace: "demo", Resource: "cm"})
	a := h.a
	h.waitFor("connected", func() bool { return a.cluster != nil })
	dyn, err := a.cluster.Conn().DynDial()
	if err != nil {
		t.Fatal(err)
	}
	cms := dyn.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("demo")
	for i := 1; i <= 3; i++ {
		_, _ = cms.Create(context.Background(), &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]any{"name": fmt.Sprintf("bulk-%d", i)},
			"data":     map[string]any{"a": "1"},
		}}, metav1.CreateOptions{})
	}
	h.waitFor("configmaps", func() bool {
		n := 0
		if a.view.snap != nil {
			for _, r := range a.view.snap.Rows {
				if strings.HasPrefix(r.ID, "demo/bulk-") {
					n++
				}
			}
		}
		return n == 3
	})
	for _, r := range a.view.snap.Rows {
		if strings.HasPrefix(r.ID, "demo/bulk-") {
			a.view.mark(r.ID, true)
		}
	}
	h.settle(80 * time.Millisecond)
	h.shot("54-multiselect")

	// right-click on a marked row → bulk menu
	idx := -1
	for i, r := range a.view.snap.Rows {
		if r.ID == "demo/bulk-2" {
			idx = i
		}
	}
	y := float32(40 + 50 + 30 + 32 + idx*30 + 15) // titlebar+toolbar rows+header+row
	h.events(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32pt(600, y)},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: f32pt(600, y)},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32pt(600, y)},
	)
	m, ok := a.overlay.(*ContextMenu)
	if !ok || !strings.Contains(m.title, "3 ") {
		t.Fatalf("expected bulk context menu, got %#v", a.overlay)
	}
	h.shot("55-bulk-menu")
	h.press(key.NameEscape)

	// bulk delete
	a.view.bulkDelete()
	cf, ok := a.overlay.(*Confirm)
	if !ok {
		t.Fatal("no bulk confirm")
	}
	h.shot("56-bulk-confirm")
	cf.accept()
	h.waitFor("bulk deleted", func() bool {
		for _, r := range a.view.snap.Rows {
			if strings.HasPrefix(r.ID, "demo/bulk-") {
				return false
			}
		}
		return true
	})

	// single-row context menu
	a.view.clearMarks()
	selectRow(t, h, "demo/settings")
	a.view.openContextMenu(a.pointer)
	if _, ok := a.overlay.(*ContextMenu); !ok {
		t.Fatal("no context menu")
	}
	h.settle(80 * time.Millisecond)
	h.shot("57-context-menu")
	h.press(key.NameEscape)
}

func TestUIWorkspace(t *testing.T) {
	dir := isolatedPrefs(t)
	h := newHarness(t, Options{Namespace: "demo", Resource: "deploy"})
	a := h.a
	selectRow(t, h, "demo/web")

	// port-forward + pin
	pf, err := a.cluster.PortForward("apps/v1/deployments", "demo/web", 0, 8080)
	if err != nil {
		t.Fatal(err)
	}
	panel := newPortForwardPanel(a)
	a.overlay = panel
	h.settle(80 * time.Millisecond)
	if len(panel.rows) != 1 || panel.rows[0].active == nil {
		t.Fatalf("panel rows %+v", panel.rows)
	}
	panel.rows[0].pin.Click()
	h.settle(80 * time.Millisecond)
	h.shot("58-portforwards")
	saved := prefs.Load().SavedForwards(a.cluster.Context)
	if len(saved) != 1 || saved[0].Local != pf.Local || !saved[0].AutoStart {
		t.Fatalf("saved %+v", saved)
	}
	a.cluster.StopPortForward(pf.ID)
	a.overlay = nil

	// bookmark with filter
	a.view.filter.SetText("web")
	a.view.applyFilter()
	a.bookmarkCurrent()
	p := a.overlay.(*Picker)
	p.pick(h.gtxForTest())
	if bms := prefs.Load().BookmarksFor(a.cluster.Context); len(bms) != 1 || bms[0].Filter != "web" {
		t.Fatalf("bookmarks %+v", bms)
	}
	a.view.filter.SetText("")
	a.view.applyFilter()

	// tabs
	a.newTab()
	a.openResource("v1/pods")
	h.waitFor("pods tab", func() bool { return a.view.gvr == "v1/pods" && a.view.snap != nil })
	h.shot("59-tabs-bookmarks")
	a.switchTab(0)
	h.waitFor("back to deploy tab", func() bool { return a.view.gvr == "apps/v1/deployments" })
	a.closeTab(1)
	if len(a.tabs) != 0 {
		t.Fatalf("tabs %d", len(a.tabs))
	}

	// saved forward restored by a new app instance
	h2 := newHarness(t, Options{Namespace: "demo", Resource: "deploy"})
	h2.waitFor("forward restored", func() bool {
		return h2.a.cluster != nil && len(h2.a.cluster.PortForwards()) == 1
	})
	_ = dir
}
