//go:build integration && dev

package backend

import "testing"

func TestLint(t *testing.T) {
	c := devCluster(t)
	c.Watch("v1/pods", "", func(*Snapshot) {}).Close()
	r := c.Lint("demo")
	rules := map[string]int{}
	for _, is := range r.Issues {
		rules[is.Rule]++
	}
	t.Logf("score %d (%s), rules %v", r.Score, r.Grade(), rules)
	for _, cs := range r.Categories {
		t.Logf("%-10s scanned %d clean %d  E%d W%d I%d", cs.Name, cs.Scanned, cs.Clean, cs.Errors, cs.Warns, cs.Infos)
	}
	for _, want := range []string{"POD-FAILING", "RES-REQUESTS", "DEP-UNAVAILABLE", "SEC-UNUSED", "CM-UNUSED"} {
		if rules[want] == 0 {
			t.Errorf("expected rule %s to fire", want)
		}
	}
}
