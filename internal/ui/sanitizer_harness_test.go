//go:build dev && integration

package ui

import (
	"testing"
	"time"

	"github.com/twoojoo/kubeon/internal/backend"
)

func TestUISanitizer(t *testing.T) {
	isolatedPrefs(t)
	h := newHarness(t, Options{Namespace: "demo"})
	a := h.a
	h.waitFor("connected", func() bool { return a.cluster != nil })
	a.openResource(sanitizerPage)
	h.waitFor("report", func() bool { return a.san.report != nil })
	h.settle(150 * time.Millisecond)
	h.shot("66-sanitizer")
	a.san.sev = 1 // errors only
	h.settle(80 * time.Millisecond)
	vis := a.san.visible()
	if len(vis) == 0 {
		t.Fatal("expected error findings in demo")
	}
	h.shot("67-sanitizer-errors")
	is := a.san.report.Issues[vis[0]]
	if is.Severity != backend.SevErr {
		t.Fatalf("filter leak %+v", is)
	}
	h.frame()
	a.san.links[vis[0]].Click()
	h.frame()
	h.waitFor("followed", func() bool { return a.view.gvr == is.GVR && a.drawer.open })
	a.drawer.close()
	a.back()
	h.waitFor("back on sanitizer", func() bool { return a.page == sanitizerPage && a.san.report != nil })
}
