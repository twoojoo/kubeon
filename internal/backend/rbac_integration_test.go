//go:build integration && dev

package backend

import "testing"

func TestRBAC(t *testing.T) {
	c := devCluster(t)
	c.Watch("rbac.authorization.k8s.io/v1/clusterrolebindings", "", func(*Snapshot) {}).Close()
	idx := c.RBAC()
	if len(idx.Subjects) < 5 {
		t.Fatalf("subjects %d", len(idx.Subjects))
	}
	admins := Subject{Kind: "Group", Name: "system:masters"}
	g, r, sub := ParseResource("pods")
	ok, rules := idx.Check(admins, Query{Verb: "delete", Group: g, Resource: r, Subresource: sub, Namespace: "demo"})
	if !ok || len(rules) == 0 {
		t.Fatal("system:masters must be able to delete pods")
	}
	def := Subject{Kind: "ServiceAccount", Name: "default", Namespace: "demo"}
	if ok, _ := idx.Check(def, Query{Verb: "delete", Group: g, Resource: r, Namespace: "demo"}); ok {
		t.Fatal("demo/default should not delete pods")
	}
	allowed, reason, err := c.AccessReview(def, Query{Verb: "delete", Group: g, Resource: r, Namespace: "demo"})
	t.Logf("SAR default delete pods: %v %q %v", allowed, reason, err)
	if err != nil || allowed {
		t.Fatalf("SAR %v %v", allowed, err)
	}
	who := idx.WhoCan(Query{Verb: "delete", Group: "", Resource: "nodes"})
	t.Logf("who can delete nodes: %d subjects", len(who))
	if len(who) == 0 {
		t.Fatal("someone must delete nodes")
	}
	gl, rl, sl := ParseResource("pods/log")
	if gl != "" || rl != "pods" || sl != "log" {
		t.Fatalf("parse pods/log: %q %q %q", gl, rl, sl)
	}
}
