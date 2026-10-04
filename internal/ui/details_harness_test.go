//go:build dev && integration

package ui

import (
	"strings"
	"testing"
	"time"
)

func TestUIDetailsOverview(t *testing.T) {
	isolatedPrefs(t)
	h := newHarness(t, Options{Namespace: "demo", Resource: "po"})
	a := h.a
	selectRow(t, h, "demo/web-")
	a.view.enter()
	h.waitFor("pod details", func() bool { return a.drawer.open && a.drawer.details != nil && a.drawer.tab == tabOverview })
	h.settle(150 * time.Millisecond)
	h.shot("60-details-pod")

	// follow the "Controlled by" link (ReplicaSet)
	var key string
	for i, f := range a.drawer.details.Sections[0].Fields {
		if f.Key == "Controlled by" {
			key = "s0/f" + itoa(i)
		}
	}
	cl := a.drawer.detV.links[key]
	if cl == nil {
		t.Fatalf("owner link clickable missing (%v)", a.drawer.detV.links)
	}
	cl.Click()
	h.frame()
	h.waitFor("replicaset details", func() bool {
		return a.view.gvr == "apps/v1/replicasets" && a.drawer.open && a.drawer.details != nil &&
			strings.HasPrefix(a.drawer.path, "demo/web-")
	})
	h.settle(150 * time.Millisecond)
	h.shot("61-details-replicaset")
	h.press("B") // back to pods via nav stack (drawer closes first with esc)
	a.drawer.close()
	a.back()
	if a.view.gvr != "v1/pods" {
		t.Fatalf("back went to %q", a.view.gvr)
	}

	a.openResource("v1/services")
	selectRow(t, h, "demo/web")
	a.view.enter()
	h.waitFor("service details", func() bool { return a.drawer.details != nil && a.drawer.path == "demo/web" })
	h.settle(150 * time.Millisecond)
	h.shot("62-details-service")
}
