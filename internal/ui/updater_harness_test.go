//go:build dev && integration

package ui

import (
	"testing"
	"time"

	"github.com/giovanni/kubeon/internal/update"
)

func TestUIUpdateDialog(t *testing.T) {
	isolatedPrefs(t)
	if updatesAllowed() {
		t.Fatal("dev builds must never self-update")
	}
	h := newHarness(t, Options{Namespace: "demo", Resource: "deploy"})
	a := h.a
	a.upd.rel = &update.Release{Version: "9.9.9", PageURL: "https://example.invalid",
		Notes: "## What's new\n\n- Rollout history with diffs\n- Faster tables on big clusters\n\n**Fixes**: log streaming of Istio pods."}
	a.upd.plan = update.Plan{Kind: update.KindAppImage, Target: "/tmp/none"}
	a.upd.downloading, a.upd.done, a.upd.total = true, 12<<20, 36<<20
	a.overlay = newUpdateDialog(a)
	h.settle(120 * time.Millisecond)
	h.shot("61-update-dialog")

	a.upd.downloading, a.upd.ready = false, true
	a.overlay = nil
	h.settle(80 * time.Millisecond)
	h.shot("62-update-ready")
	a.updBtn.Click()
	h.frame()
	if _, ok := a.overlay.(*UpdateDialog); !ok {
		t.Fatalf("status chip should open the dialog, overlay %T", a.overlay)
	}
}
