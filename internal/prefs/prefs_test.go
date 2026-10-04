package prefs

import "testing"

func TestLooksProduction(t *testing.T) {
	for _, c := range []string{"prod", "eks-prod-eu", "gke_acme_production", "aks-prd", "live-cluster", "PROD"} {
		if !LooksProduction(c) {
			t.Errorf("%q should look like production", c)
		}
	}
	for _, c := range []string{"kind-k9desk-dev", "staging", "product-team-dev", "olive"} {
		if LooksProduction(c) {
			t.Errorf("%q should not look like production", c)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	t.Setenv("K9DESK_CONFIG_DIR", t.TempDir())
	p := Load()
	if p.Theme != "dark" || !p.Protected("prod") || p.Protected("dev") {
		t.Fatal("defaults")
	}
	p.Theme = "light"
	p.SetPolicy("prod", false, true)
	p.SavePortForward(PortForward{Context: "c", GVR: "v1/pods", Path: "ns/p", Local: 8080, Remote: 80, AutoStart: true})
	p.SavePortForward(PortForward{Context: "c", GVR: "v1/pods", Path: "ns/p", Local: 9090, Remote: 80, AutoStart: true})
	p.AddBookmark(Bookmark{Name: "web pods", GVR: "v1/pods", Namespace: "demo", Filter: "web"})
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	q := Load()
	if q.Theme != "light" || q.Protected("prod") || !q.ReadOnly("prod") {
		t.Fatalf("policy %+v", q.Contexts)
	}
	if f := q.SavedForwards("c"); len(f) != 1 || f[0].Local != 9090 {
		t.Fatalf("forwards %+v", f)
	}
	if b := q.BookmarksFor("any"); len(b) != 1 || b[0].Filter != "web" {
		t.Fatalf("bookmarks %+v", b)
	}
}
