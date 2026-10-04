//go:build dev && integration

package ui

import (
	"strings"
	"testing"
	"time"
)

func TestUIRBACExplorer(t *testing.T) {
	isolatedPrefs(t)
	h := newHarness(t, Options{Namespace: "demo"})
	a := h.a
	h.waitFor("connected", func() bool { return a.cluster != nil })
	a.openResource(rbacPage)
	h.waitFor("rbac index", func() bool { return a.rbac.idx != nil })

	// select system:masters group
	for i, s := range a.rbac.idx.Subjects {
		if s.Name == "system:masters" {
			a.rbac.sel = i
			a.rbac.refreshRules()
		}
	}
	a.rbac.verb.SetText("delete")
	a.rbac.res.SetText("pods")
	a.rbac.check()
	h.waitFor("sar", func() bool { return a.rbac.sar != "…" })
	if !a.rbac.localOK || !strings.HasPrefix(a.rbac.sar, "allowed") {
		t.Fatalf("masters: local=%v sar=%q", a.rbac.localOK, a.rbac.sar)
	}
	h.settle(100 * time.Millisecond)
	h.shot("68-rbac-subject")

	// demo/default cannot delete pods
	for i, s := range a.rbac.idx.Subjects {
		if s.Kind == "ServiceAccount" && s.Namespace == "demo" && s.Name == "default" {
			a.rbac.sel = i
			a.rbac.refreshRules()
		}
	}
	a.rbac.check()
	h.waitFor("sar2", func() bool { return a.rbac.sar != "…" })
	if a.rbac.localOK || !strings.HasPrefix(a.rbac.sar, "denied") {
		t.Fatalf("default: local=%v sar=%q", a.rbac.localOK, a.rbac.sar)
	}

	// who can delete nodes
	a.rbac.who = true
	a.rbac.verb.SetText("delete")
	a.rbac.res.SetText("nodes")
	a.rbac.ns.SetText("")
	a.rbac.check()
	if len(a.rbac.whoRes) == 0 {
		t.Fatal("nobody can delete nodes?")
	}
	h.settle(100 * time.Millisecond)
	h.shot("69-rbac-whocan")
}
