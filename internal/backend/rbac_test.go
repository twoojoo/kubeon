package backend

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
)

func TestRuleAllowsSubresources(t *testing.T) {
	rule := func(res ...string) rbacv1.PolicyRule {
		return rbacv1.PolicyRule{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: res}
	}
	cases := []struct {
		rule     rbacv1.PolicyRule
		res, sub string
		want     bool
	}{
		{rule("pods"), "pods", "", true},
		{rule("pods"), "pods", "log", false},
		{rule("pods/log"), "pods", "log", true},
		{rule("pods/*"), "pods", "log", false}, // not a wildcard for the authorizer
		{rule("*/scale"), "deployments", "scale", true},
		{rule("*"), "pods", "log", true},
	}
	for _, c := range cases {
		if got := ruleAllows(c.rule, "get", "", c.res, c.sub, ""); got != c.want {
			t.Errorf("%v %s/%s: got %v want %v", c.rule.Resources, c.res, c.sub, got, c.want)
		}
	}
}

func TestMatchesServiceAccountAsUser(t *testing.T) {
	sa := Subject{Kind: rbacv1.ServiceAccountKind, Namespace: "demo", Name: "app"}
	ok, _ := matchesSubject(rbacv1.Subject{Kind: rbacv1.UserKind, Name: "system:serviceaccount:demo:app"}, "", sa, nil)
	if !ok {
		t.Fatal("User subject with the SA user name must match the SA")
	}
}
