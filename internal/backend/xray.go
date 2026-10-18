package backend

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/twoojoo/kubeon/third_party/k9s/client"
	"github.com/twoojoo/kubeon/third_party/k9s/model"
	"github.com/twoojoo/kubeon/third_party/k9s/xray"
)

// XNode is an immutable xray tree node (k9s xray renderers).
type XNode struct {
	GVR      string // e.g. apps/v1/deployments, containers
	Kind     string // resource plural (deployments, pods, …)
	ID       string
	Name     string
	Status   string // ok | toast | toast_kids | noref …
	Info     string
	Children []*XNode
}

// Healthy reports whether the node itself is fine.
func (n *XNode) Healthy() bool { return n.Status == xray.OkStatus || n.Status == "" }

// Count returns the number of nodes in the subtree.
func (n *XNode) Count() int {
	c := 1
	for _, k := range n.Children {
		c += k.Count()
	}
	return c
}

// CanXRay reports whether k9s has a tree renderer for gvr.
func CanXRay(gvr string) bool {
	m, ok := model.Registry[client.NewGVR(gvr)]
	return ok && m.TreeRenderer != nil
}

// XRayWatch keeps a k9s xray tree live.
type XRayWatch struct {
	tree   *model.Tree
	cancel context.CancelFunc
	onTree func(*XNode, error)
	closed atomic.Bool
}

// XRay watches the dependency tree of every gvr object in ns.
func (c *Cluster) XRay(gvr, ns string, onTree func(*XNode, error)) *XRayWatch {
	g := client.NewGVR(gvr)
	if ns == "" {
		ns = client.BlankNamespace
	}
	w := XRayWatch{tree: model.NewTree(g), onTree: onTree}
	w.tree.SetNamespace(ns)
	w.tree.SetRefreshRate(2 * time.Second)
	w.tree.AddListener(&w)
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.tree.Watch(c.baseContext(ctx, g, ns, ""))
	return &w
}

// Close stops the watch.
func (w *XRayWatch) Close() {
	if w.closed.Swap(true) {
		return
	}
	w.tree.RemoveListener(w)
	w.cancel()
}

// TreeChanged implements model.TreeListener.
func (w *XRayWatch) TreeChanged(root *xray.TreeNode) {
	if !w.closed.Load() {
		w.onTree(convertX(root), nil)
	}
}

// TreeLoadFailed implements model.TreeListener.
func (w *XRayWatch) TreeLoadFailed(err error) {
	if !w.closed.Load() {
		w.onTree(nil, err)
	}
}

func convertX(t *xray.TreeNode) *XNode {
	if t == nil {
		return nil
	}
	n := XNode{
		GVR:    t.GVR.String(),
		Kind:   t.GVR.R(),
		ID:     t.ID,
		Name:   t.ID,
		Status: t.Extras[xray.StatusKey],
		Info:   t.Extras[xray.InfoKey],
	}
	if i := strings.LastIndexByte(t.ID, '/'); i >= 0 {
		n.Name = t.ID[i+1:]
	}
	n.Children = make([]*XNode, 0, len(t.Children))
	for _, c := range t.Children {
		n.Children = append(n.Children, convertX(c))
	}
	return &n
}
