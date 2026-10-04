//go:build dev && integration

package ui

import (
	"strings"
	"testing"
	"time"
)

func TestUIYAMLEditDiffApply(t *testing.T) {
	isolatedPrefs(t)
	h := newHarness(t, Options{Namespace: "demo", Resource: "cm"})
	a := h.a
	selectRow(t, h, "demo/settings")
	a.view.details(tabYAML)
	h.waitFor("yaml", func() bool { return a.drawer.open && len(a.drawer.yamlV.lines) > 5 })
	h.settle(100 * time.Millisecond)
	h.shot("63-yaml-highlight")

	a.view.edit()
	h.frame()
	orig := a.drawer.yaml.Text()
	if !strings.Contains(orig, "level: debug") {
		t.Fatalf("unexpected yaml:\n%s", orig)
	}
	// invalid YAML is caught live
	a.drawer.yaml.SetText(orig + "\n  bad: [unclosed\n")
	h.frame()
	h.frame()
	if a.drawer.valMsg == "" {
		t.Fatal("invalid yaml not detected")
	}
	h.shot("64-yaml-invalid")

	// a valid change → diff review → dry run → apply
	stamp := time.Now().Format("150405")
	edited := strings.Replace(orig, "level: debug", "level: info-"+stamp, 1)
	a.drawer.yaml.SetText(edited)
	h.frame()
	h.frame()
	if a.drawer.valMsg != "" || !a.drawer.dirty {
		t.Fatalf("valid edit flagged: %q dirty=%v", a.drawer.valMsg, a.drawer.dirty)
	}
	a.drawer.saveBtn.Click()
	h.frame()
	r, ok := a.overlay.(*DiffReview)
	if !ok {
		t.Fatalf("expected diff review, got %#v", a.overlay)
	}
	h.waitFor("dry run", func() bool { return r.dry != "running" })
	if r.dry != "ok" || r.add != 1 || r.del != 1 {
		t.Fatalf("dry=%q +%d -%d", r.dry, r.add, r.del)
	}
	h.shot("65-diff-review")
	r.applyBtn.Click()
	h.frame()
	h.waitFor("applied", func() bool {
		y, _ := a.cluster.YAML("v1/configmaps", "demo/settings")
		return strings.Contains(y, "level: info-"+stamp)
	})

	// restore
	y, _ := a.cluster.YAML("v1/configmaps", "demo/settings")
	if err := a.cluster.Apply("v1/configmaps", strings.Replace(y, "level: info-"+stamp, "level: debug", 1)); err != nil {
		t.Fatal(err)
	}
}
