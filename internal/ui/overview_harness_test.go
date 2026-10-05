//go:build dev && integration

package ui

import (
	"testing"
	"time"
)

func TestUIOverviewLinks(t *testing.T) {
	isolatedPrefs(t)
	h := newHarness(t, Options{})
	a := h.a
	h.waitFor("overview", func() bool { return a.ovw.data != nil && len(a.ovw.data.Warnings) > 0 })
	targets := map[int]string{0: "v1/nodes", 1: "v1/pods", 2: "apps/v1/deployments", 3: "v1/namespaces"}
	for i, gvr := range targets {
		a.openResource(overviewGVR)
		h.waitFor("overview again", func() bool { return a.ovw.data != nil })
		a.ovw.cards4[i].Click()
		h.frame()
		h.frame()
		if a.page != gvr || a.view.gvr != gvr {
			t.Fatalf("card %d: page %q view %q, want %q", i, a.page, a.view.gvr, gvr)
		}
	}
	// a warning row opens the involved object
	a.openResource(overviewGVR)
	h.waitFor("warnings", func() bool { return a.ovw.data != nil && len(a.ovw.warnCl) > 0 })
	w := a.ovw.data.Warnings[0]
	gvr, path, ok := warningTarget(&w)
	if !ok {
		t.Fatalf("warning %+v not linkable", w)
	}
	a.ovw.warnCl[warnKey(&w)].Click()
	h.frame()
	h.frame()
	if a.view.gvr != gvr || a.view.selID != path {
		t.Fatalf("warning link: view %q sel %q, want %q %q", a.view.gvr, a.view.selID, gvr, path)
	}
	h.settle(200 * time.Millisecond)
}
