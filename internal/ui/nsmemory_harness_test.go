//go:build dev && integration

package ui

import (
	"testing"

	"github.com/twoojoo/kubeon/internal/prefs"
)

func TestUINamespacePerContext(t *testing.T) {
	isolatedPrefs(t)
	h := newHarness(t, Options{})
	a := h.a
	h.waitFor("connected", func() bool { return a.cluster != nil })
	if a.ns != "" {
		t.Fatalf("fresh prefs: ns %q", a.ns)
	}
	a.setNamespace("kube-system")
	ctx := a.cluster.Context
	if got := prefs.Load().LastNamespace(ctx); got != "kube-system" {
		t.Fatalf("saved %q", got)
	}

	// a restart reopens the context on its namespace
	h2 := newHarness(t, Options{})
	h2.waitFor("connected", func() bool { return h2.a.cluster != nil })
	if h2.a.ns != "kube-system" {
		t.Fatalf("restored %q", h2.a.ns)
	}

	// a namespace deleted since falls back to all namespaces
	p := prefs.Load()
	p.SetLastNamespace(ctx, "gone-since")
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	h3 := newHarness(t, Options{})
	h3.waitFor("connected", func() bool { return h3.a.cluster != nil })
	if h3.a.ns != "" {
		t.Fatalf("missing namespace kept: %q", h3.a.ns)
	}

	// --namespace wins over the saved one
	h4 := newHarness(t, Options{Namespace: "demo"})
	h4.waitFor("connected", func() bool { return h4.a.cluster != nil })
	if h4.a.ns != "demo" || h4.a.opts.Namespace != "" {
		t.Fatalf("flag: ns %q opts %q", h4.a.ns, h4.a.opts.Namespace)
	}
}
