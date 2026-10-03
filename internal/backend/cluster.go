// Package backend is a thin facade over the vendored k9s engine
// (client, watch, dao, model, render). The UI only talks to this package.
package backend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/tools/clientcmd"

	k9s "github.com/giovanni/k9desk/third_party/k9s"
	"github.com/giovanni/k9desk/third_party/k9s/client"
	"github.com/giovanni/k9desk/third_party/k9s/dao"
	"github.com/giovanni/k9desk/third_party/k9s/watch"
)

const callTimeout = "15s"

// Contexts lists the contexts declared in the given kubeconfig file only.
// The file is never modified.
func Contexts(kubeconfig string) (names []string, current string, err error) {
	if kubeconfig == "" {
		return nil, "", errors.New("no kubeconfig path given")
	}
	cfg, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		return nil, "", err
	}
	for n := range cfg.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)

	return names, cfg.CurrentContext, nil
}

// Cluster is a live connection to one kube context, backed by k9s
// informers (watch.Factory).
type Cluster struct {
	Kubeconfig string
	Context    string

	cfg     *client.Config
	conn    *client.APIClient
	factory *watch.Factory

	pf      forwards
	version string
}

// Connect dials the given context of the given kubeconfig file.
// Only that explicit file is read: $KUBECONFIG and ~/.kube/config are ignored.
func Connect(kubeconfig, kctx string) (*Cluster, error) {
	if kubeconfig == "" {
		return nil, errors.New("no kubeconfig path given")
	}
	flags := genericclioptions.NewConfigFlags(false)
	kc, ctxName, timeout := kubeconfig, kctx, callTimeout
	flags.KubeConfig = &kc
	flags.Timeout = &timeout
	if ctxName != "" {
		flags.Context = &ctxName
	}
	cfg := client.NewConfig(flags)
	if ctxName == "" {
		n, err := cfg.CurrentContextName()
		if err != nil {
			return nil, err
		}
		ctxName = n
	}
	conn, err := client.InitConnection(cfg, slog.Default())
	if err != nil && (conn == nil || !conn.ConnectionOK()) {
		return nil, fmt.Errorf("connect %q: %w", ctxName, err)
	}
	if !conn.CheckConnectivity() {
		return nil, fmt.Errorf("cluster %q unreachable", ctxName)
	}

	f := watch.NewFactory(conn)
	f.Start(client.BlankNamespace)
	c := Cluster{
		Kubeconfig: kubeconfig,
		Context:    ctxName,
		cfg:        cfg,
		conn:       conn,
		factory:    f,
	}
	if err := c.RefreshResources(); err != nil {
		f.Terminate()
		return nil, fmt.Errorf("load resources: %w", err)
	}

	if v, err := conn.ServerVersion(); err == nil {
		c.version = v.GitVersion
	}

	return &c, nil
}

// RefreshResources drops the discovery cache and reloads k9s resource metas
// (picks up newly installed CRDs).
func (c *Cluster) RefreshResources() error {
	if d, err := c.conn.CachedDiscovery(); err == nil {
		d.Invalidate()
	}
	return dao.MetaAccess.LoadResources(c.factory)
}

// Close stops all informers and port-forwards.
func (c *Cluster) Close() {
	c.stopAllForwards()
	c.factory.Terminate()
}

// Version returns the api-server version.
func (c *Cluster) Version() string { return c.version }

// HasMetrics reports whether metrics-server is available.
func (c *Cluster) HasMetrics() bool { return c.conn.HasMetrics() }

// Conn exposes the k9s connection.
func (c *Cluster) Conn() client.Connection { return c.conn }

// Factory exposes the k9s informer factory.
func (c *Cluster) Factory() *watch.Factory { return c.factory }

// Namespaces returns the namespace names visible to the user.
func (c *Cluster) Namespaces() ([]string, error) {
	oo, err := c.factory.List(client.NsGVR, client.ClusterScope, true, labels.Everything())
	if err != nil {
		nn, e := c.conn.ValidNamespaceNames()
		if e != nil {
			return nil, err
		}
		out := make([]string, 0, len(nn))
		for n := range nn {
			out = append(out, n)
		}
		sort.Strings(out)
		return out, nil
	}
	out := make([]string, 0, len(oo))
	for _, o := range oo {
		if fqn := objectName(o); fqn != "" {
			out = append(out, fqn)
		}
	}
	sort.Strings(out)

	return out, nil
}

// baseContext builds the context k9s dao/model expect.
func (c *Cluster) baseContext(ctx context.Context, gvr *client.GVR, ns, path string) context.Context {
	ctx = context.WithValue(ctx, k9s.KeyFactory, c.factory)
	ctx = context.WithValue(ctx, k9s.KeyGVR, gvr)
	ctx = context.WithValue(ctx, k9s.KeyPath, path)
	if path != "" {
		ctx = context.WithValue(ctx, k9s.KeyFQN, path)
	}
	ctx = context.WithValue(ctx, k9s.KeyNamespace, client.CleanseNamespace(ns))
	ctx = context.WithValue(ctx, k9s.KeyWithMetrics, c.conn.HasMetrics())

	return ctx
}

// ResourceInfo describes a discovered resource kind.
type ResourceInfo struct {
	GVR        string
	Kind       string
	Name       string
	Group      string
	Namespaced bool
	CRD        bool
	Verbs      []string
}

// Resources lists API resources known to k9s' registry (core + CRDs).
func (c *Cluster) Resources() []ResourceInfo {
	var out []ResourceInfo
	for _, gvr := range dao.MetaAccess.AllGVRs() {
		m, err := dao.MetaAccess.MetaFor(gvr)
		if err != nil || dao.IsK9sMeta(m) || !dao.IsK8sMeta(m) {
			continue
		}
		if !strings.Contains(gvr.String(), "/") {
			continue
		}
		out = append(out, ResourceInfo{
			GVR:        gvr.String(),
			Kind:       m.Kind,
			Name:       m.Name,
			Group:      gvr.G(),
			Namespaced: m.Namespaced,
			CRD:        dao.IsCRD(m),
			Verbs:      m.Verbs,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Kind < out[j].Kind
	})

	return out
}

// IsNamespaced reports whether the gvr is namespaced.
func IsNamespaced(gvr string) bool {
	ok, err := dao.MetaAccess.IsNamespaced(client.NewGVR(gvr))
	return err != nil || ok
}

// CanVerb reports whether the gvr supports a verb (per discovery).
func CanVerb(gvr, verb string) bool {
	m, err := dao.MetaAccess.MetaFor(client.NewGVR(gvr))
	if err != nil {
		return false
	}
	return client.Can(m.Verbs, verb)
}

func timeoutCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

var extraAliases = map[string]string{
	"dp":    "apps/v1/deployments",
	"pod":   "v1/pods",
	"crd":   "apiextensions.k8s.io/v1/customresourcedefinitions",
	"crds":  "apiextensions.k8s.io/v1/customresourcedefinitions",
	"ev":    "events.k8s.io/v1/events",
	"event": "events.k8s.io/v1/events",
}

// LookupAlias resolves a k9s-style command (po, deploy, svc, CRD names,
// full gvr) to a gvr.
func LookupAlias(cmd string) (string, bool) {
	cmd = strings.ToLower(cmd)
	if g, ok := extraAliases[cmd]; ok {
		return g, true
	}
	if strings.Count(cmd, "/") >= 1 {
		if _, err := dao.MetaAccess.MetaFor(client.NewGVR(cmd)); err == nil {
			return cmd, true
		}
	}
	if g := dao.MetaAccess.Lookup(cmd); g != client.NoGVR {
		return g.String(), true
	}
	for _, r := range lookupFallback() {
		if strings.EqualFold(r.Kind, cmd) || r.Name == cmd || r.Name+"."+r.Group == cmd {
			return r.GVR, true
		}
	}
	return "", false
}

func lookupFallback() []ResourceInfo {
	return (&Cluster{}).Resources()
}
