package backend

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	authzv1 "k8s.io/api/authorization/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/giovanni/k9desk/third_party/k9s/client"
)

// Subject is an RBAC subject.
type Subject struct {
	Kind      string // User | Group | ServiceAccount
	Name      string
	Namespace string // service accounts only
	Bindings  int
}

// Key identifies the subject.
func (s Subject) Key() string {
	if s.Kind == rbacv1.ServiceAccountKind {
		return "ServiceAccount:" + s.Namespace + "/" + s.Name
	}
	return s.Kind + ":" + s.Name
}

// Display is a human label.
func (s Subject) Display() string {
	if s.Kind == rbacv1.ServiceAccountKind {
		return s.Namespace + "/" + s.Name
	}
	return s.Name
}

// identity returns the user and groups the API server sees for a subject.
func (s Subject) identity() (string, []string) {
	switch s.Kind {
	case rbacv1.ServiceAccountKind:
		return "system:serviceaccount:" + s.Namespace + ":" + s.Name,
			[]string{"system:serviceaccounts", "system:serviceaccounts:" + s.Namespace, "system:authenticated"}
	case rbacv1.GroupKind:
		return "", []string{s.Name}
	default:
		return s.Name, []string{"system:authenticated"}
	}
}

// EffectiveRule is a policy rule granted to a subject, with provenance.
type EffectiveRule struct {
	Namespace string // "" = cluster-wide
	Rule      rbacv1.PolicyRule
	Binding   Link // RoleBinding/ClusterRoleBinding
	Role      Link // Role/ClusterRole
	ViaGroup  string
}

// RBACIndex is a snapshot of bindings and roles.
type RBACIndex struct {
	roles        map[string]rbacv1.Role // ns/name
	clusterRoles map[string]rbacv1.ClusterRole
	rbs          []rbacv1.RoleBinding
	crbs         []rbacv1.ClusterRoleBinding
	Subjects     []Subject
}

// RBAC builds an index from the informer caches.
func (c *Cluster) RBAC() *RBACIndex {
	idx := &RBACIndex{roles: map[string]rbacv1.Role{}, clusterRoles: map[string]rbacv1.ClusterRole{}}
	for _, r := range listTyped[rbacv1.Role](c, client.RoGVR, client.BlankNamespace) {
		idx.roles[r.Namespace+"/"+r.Name] = r
	}
	for _, r := range listTyped[rbacv1.ClusterRole](c, client.CrGVR, client.ClusterScope) {
		idx.clusterRoles[r.Name] = r
	}
	idx.rbs = listTyped[rbacv1.RoleBinding](c, client.RobGVR, client.BlankNamespace)
	idx.crbs = listTyped[rbacv1.ClusterRoleBinding](c, client.CrbGVR, client.ClusterScope)

	subs := map[string]*Subject{}
	add := func(s rbacv1.Subject, bindingNS string) {
		sub := Subject{Kind: s.Kind, Name: s.Name}
		if s.Kind == rbacv1.ServiceAccountKind {
			sub.Namespace = s.Namespace
			if sub.Namespace == "" {
				sub.Namespace = bindingNS
			}
		}
		if p := subs[sub.Key()]; p != nil {
			p.Bindings++
			return
		}
		sub.Bindings = 1
		subs[sub.Key()] = &sub
	}
	for _, b := range idx.rbs {
		for _, s := range b.Subjects {
			add(s, b.Namespace)
		}
	}
	for _, b := range idx.crbs {
		for _, s := range b.Subjects {
			add(s, "")
		}
	}
	// service accounts without bindings still matter (implicit groups)
	for _, sa := range listTyped[v1.ServiceAccount](c, client.SaGVR, client.BlankNamespace) {
		s := Subject{Kind: rbacv1.ServiceAccountKind, Name: sa.Name, Namespace: sa.Namespace}
		if subs[s.Key()] == nil {
			subs[s.Key()] = &s
		}
	}
	for _, s := range subs {
		idx.Subjects = append(idx.Subjects, *s)
	}
	kindRank := map[string]int{rbacv1.UserKind: 0, rbacv1.GroupKind: 1, rbacv1.ServiceAccountKind: 2}
	sort.Slice(idx.Subjects, func(i, j int) bool {
		a, b := idx.Subjects[i], idx.Subjects[j]
		if a.Kind != b.Kind {
			return kindRank[a.Kind] < kindRank[b.Kind]
		}
		return a.Display() < b.Display()
	})
	return idx
}

func matchesSubject(s rbacv1.Subject, bindingNS string, sub Subject, groups []string) (bool, string) {
	switch s.Kind {
	case rbacv1.GroupKind:
		if sub.Kind == rbacv1.GroupKind && s.Name == sub.Name {
			return true, ""
		}
		if slices.Contains(groups, s.Name) && sub.Kind != rbacv1.GroupKind {
			return true, s.Name
		}
	case rbacv1.UserKind:
		return sub.Kind == rbacv1.UserKind && s.Name == sub.Name, ""
	case rbacv1.ServiceAccountKind:
		ns := s.Namespace
		if ns == "" {
			ns = bindingNS
		}
		return sub.Kind == rbacv1.ServiceAccountKind && s.Name == sub.Name && ns == sub.Namespace, ""
	}
	return false, ""
}

func (idx *RBACIndex) roleRules(ref rbacv1.RoleRef, ns string) ([]rbacv1.PolicyRule, Link) {
	if ref.Kind == "ClusterRole" {
		return idx.clusterRoles[ref.Name].Rules, Link{GVR: "rbac.authorization.k8s.io/v1/clusterroles", Path: ref.Name}
	}
	return idx.roles[ns+"/"+ref.Name].Rules, Link{GVR: "rbac.authorization.k8s.io/v1/roles", Path: ns + "/" + ref.Name}
}

// Rules returns the effective rules of a subject.
func (idx *RBACIndex) Rules(sub Subject) []EffectiveRule {
	_, groups := sub.identity()
	var out []EffectiveRule
	for _, b := range idx.crbs {
		for _, s := range b.Subjects {
			if ok, via := matchesSubject(s, "", sub, groups); ok {
				rules, role := idx.roleRules(b.RoleRef, "")
				for _, r := range rules {
					out = append(out, EffectiveRule{Rule: r, Role: role, ViaGroup: via,
						Binding: Link{GVR: "rbac.authorization.k8s.io/v1/clusterrolebindings", Path: b.Name}})
				}
				break
			}
		}
	}
	for _, b := range idx.rbs {
		for _, s := range b.Subjects {
			if ok, via := matchesSubject(s, b.Namespace, sub, groups); ok {
				rules, role := idx.roleRules(b.RoleRef, b.Namespace)
				for _, r := range rules {
					out = append(out, EffectiveRule{Namespace: b.Namespace, Rule: r, Role: role, ViaGroup: via,
						Binding: Link{GVR: "rbac.authorization.k8s.io/v1/rolebindings", Path: b.Namespace + "/" + b.Name}})
				}
				break
			}
		}
	}
	return out
}

func has(list []string, v string) bool {
	return slices.Contains(list, "*") || slices.Contains(list, v)
}

// Allows reports whether a rule grants verb on group/resource[/sub].
func ruleAllows(r rbacv1.PolicyRule, verb, group, resource, sub, name string) bool {
	if !has(r.Verbs, verb) || !has(r.APIGroups, group) {
		return false
	}
	res := resource
	if sub != "" {
		res = resource + "/" + sub
	}
	if !has(r.Resources, res) && !(sub != "" && slices.Contains(r.Resources, resource+"/*")) {
		return false
	}
	return len(r.ResourceNames) == 0 || (name != "" && slices.Contains(r.ResourceNames, name))
}

// Query is a can-i question.
type Query struct {
	Verb, Group, Resource, Subresource, Name, Namespace string
}

// ParseResource resolves "pods", "deploy", "pods/log" to group+resource.
func ParseResource(s string) (group, resource, sub string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '/'); i > 0 && !strings.Contains(s[i+1:], "/") {
		if _, ok := LookupAlias(s[:i]); ok {
			s, sub = s[:i], s[i+1:]
		}
	}
	if g, ok := LookupAlias(s); ok {
		gv := client.NewGVR(g)
		return gv.G(), gv.R(), sub
	}
	return "", s, sub
}

// Check evaluates a query locally and returns the matching rules.
func (idx *RBACIndex) Check(sub Subject, q Query) (bool, []EffectiveRule) {
	var match []EffectiveRule
	for _, er := range idx.Rules(sub) {
		if er.Namespace != "" && er.Namespace != q.Namespace {
			continue
		}
		if ruleAllows(er.Rule, q.Verb, q.Group, q.Resource, q.Subresource, q.Name) {
			match = append(match, er)
		}
	}
	return len(match) > 0, match
}

// WhoCan lists subjects allowed by local evaluation.
func (idx *RBACIndex) WhoCan(q Query) []Subject {
	var out []Subject
	for _, s := range idx.Subjects {
		if ok, _ := idx.Check(s, q); ok {
			out = append(out, s)
		}
	}
	return out
}

// AccessReview asks the API server (SubjectAccessReview). Authoritative,
// needs permission to create subjectaccessreviews.
func (c *Cluster) AccessReview(sub Subject, q Query) (bool, string, error) {
	cs, err := c.conn.Dial()
	if err != nil {
		return false, "", err
	}
	user, groups := sub.identity()
	sar := &authzv1.SubjectAccessReview{Spec: authzv1.SubjectAccessReviewSpec{
		User:   user,
		Groups: groups,
		ResourceAttributes: &authzv1.ResourceAttributes{
			Namespace: q.Namespace, Verb: q.Verb, Group: q.Group,
			Resource: q.Resource, Subresource: q.Subresource, Name: q.Name,
		},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := cs.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
	if err != nil {
		return false, "", err
	}
	reason := res.Status.Reason
	if res.Status.EvaluationError != "" {
		reason += " (" + res.Status.EvaluationError + ")"
	}
	return res.Status.Allowed, reason, nil
}

// RuleString renders a rule compactly.
func RuleString(r rbacv1.PolicyRule) (verbs, resources string) {
	verbs = strings.Join(r.Verbs, ",")
	if len(r.NonResourceURLs) > 0 {
		return verbs, "urls: " + strings.Join(r.NonResourceURLs, ",")
	}
	var res []string
	for _, g := range r.APIGroups {
		for _, rr := range r.Resources {
			s := rr
			if g != "" && g != "*" {
				s = rr + "." + g
			} else if g == "*" {
				s = rr + ".*"
			}
			res = append(res, s)
		}
	}
	resources = strings.Join(res, ", ")
	if len(r.ResourceNames) > 0 {
		resources += fmt.Sprintf(" [%s]", strings.Join(r.ResourceNames, ","))
	}
	return verbs, resources
}
