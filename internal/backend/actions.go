package backend

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	k9s "github.com/giovanni/kubeon/third_party/k9s"
	"github.com/giovanni/kubeon/third_party/k9s/client"
	"github.com/giovanni/kubeon/third_party/k9s/dao"
)

func cloneAccessor(a dao.Accessor) dao.Accessor {
	t := reflect.TypeOf(a)
	if t.Kind() != reflect.Pointer {
		return a
	}
	if na, ok := reflect.New(t.Elem()).Interface().(dao.Accessor); ok {
		return na
	}
	return a
}

func labelSelector(q string) (labels.Selector, bool) {
	if !k9s.IsLabelSelector(q) {
		return nil, false
	}
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(q), "-l"))
	sel, err := labels.Parse(s)
	if err != nil {
		return nil, false
	}
	return sel, true
}

func objectName(o runtime.Object) string {
	if u, ok := o.(*unstructured.Unstructured); ok {
		return u.GetName()
	}
	if m, ok := o.(metav1.Object); ok {
		return m.GetName()
	}
	return ""
}

// YAML returns the manifest of a resource (managed fields stripped).
func (c *Cluster) YAML(gvr, path string) (string, error) {
	a := c.Accessor(gvr)
	if d, ok := a.(dao.Describer); ok {
		return d.ToYAML(path, false)
	}
	g := dao.Generic{}
	g.Init(c.factory, client.NewGVR(gvr))
	return g.ToYAML(path, false)
}

// Describe returns kubectl-describe output (or the DAO's own description,
// e.g. for helm releases).
func (c *Cluster) Describe(gvr, path string) (string, error) {
	if gvr == HelmGVR || gvr == HelmHistoryGVR {
		return c.HelmDescribe(path)
	}
	if !strings.Contains(gvr, "/") {
		if d, ok := c.Accessor(gvr).(dao.Describer); ok {
			return d.Describe(path)
		}
	}
	return dao.Describe(c.conn, client.NewGVR(gvr), path)
}

// Delete deletes a resource with background propagation.
func (c *Cluster) Delete(gvr, path string, force bool) error {
	n, ok := c.Accessor(gvr).(dao.Nuker)
	if !ok {
		return fmt.Errorf("%s cannot be deleted", gvr)
	}
	p := metav1.DeletePropagationBackground
	grace := dao.DefaultGrace
	if force {
		grace = dao.ForceGrace
	}
	ctx, cancel := timeoutCtx()
	defer cancel()
	return n.Delete(c.baseContext(ctx, client.NewGVR(gvr), "", path), path, &p, grace)
}

// CanScale reports whether gvr supports scaling.
func (c *Cluster) CanScale(gvr string) bool {
	_, ok := c.Accessor(gvr).(dao.Scalable)
	return ok
}

// CanRestart reports whether gvr supports rollout restart.
func (c *Cluster) CanRestart(gvr string) bool {
	_, ok := c.Accessor(gvr).(dao.Restartable)
	return ok
}

// Replicas returns current desired replicas.
func (c *Cluster) Replicas(gvr, path string) (int32, error) {
	r, ok := c.Accessor(gvr).(dao.ReplicasGetter)
	if !ok {
		return 0, fmt.Errorf("%s has no replicas", gvr)
	}
	ctx, cancel := timeoutCtx()
	defer cancel()
	return r.Replicas(c.baseContext(ctx, client.NewGVR(gvr), "", path), path)
}

// Scale scales a workload.
func (c *Cluster) Scale(gvr, path string, replicas int32) error {
	s, ok := c.Accessor(gvr).(dao.Scalable)
	if !ok {
		return fmt.Errorf("%s cannot be scaled", gvr)
	}
	ctx, cancel := timeoutCtx()
	defer cancel()
	return s.Scale(c.baseContext(ctx, client.NewGVR(gvr), "", path), path, replicas)
}

// Restart performs a rollout restart.
func (c *Cluster) Restart(gvr, path string) error {
	r, ok := c.Accessor(gvr).(dao.Restartable)
	if !ok {
		return fmt.Errorf("%s cannot be restarted", gvr)
	}
	ctx, cancel := timeoutCtx()
	defer cancel()
	return r.Restart(c.baseContext(ctx, client.NewGVR(gvr), "", path), path, &metav1.PatchOptions{FieldManager: "kubeon"})
}

// Apply updates (or creates, when missing) a resource from a YAML manifest.
func (c *Cluster) Apply(gvr, manifest string) error { return c.apply(gvr, manifest, false) }

// ApplyDryRun validates a manifest server-side without persisting it.
func (c *Cluster) ApplyDryRun(gvr, manifest string) error { return c.apply(gvr, manifest, true) }

var yamlDocSep = regexp.MustCompile(`(?m)^---[ \t]*$`)

func (c *Cluster) apply(gvr, manifest string, dryRun bool) error {
	docs := 0
	for _, d := range yamlDocSep.Split(manifest, -1) {
		if strings.TrimSpace(d) != "" {
			docs++
		}
	}
	if docs > 1 {
		return errors.New("the manifest holds several objects: apply one at a time")
	}
	manifest = strings.TrimPrefix(strings.TrimSpace(manifest), "---")
	var obj map[string]any
	if err := yaml.Unmarshal([]byte(manifest), &obj); err != nil {
		return fmt.Errorf("invalid yaml: %w", err)
	}
	if obj == nil {
		return errors.New("empty manifest")
	}
	u := unstructured.Unstructured{Object: obj}
	if u.GetName() == "" {
		return errors.New("manifest has no metadata.name")
	}
	dial, err := c.conn.DynDial()
	if err != nil {
		return err
	}
	var ri dynamic.ResourceInterface = dial.Resource(client.NewGVR(gvr).GVR())
	switch ns := u.GetNamespace(); {
	case ns != "":
		ri = dial.Resource(client.NewGVR(gvr).GVR()).Namespace(ns)
	case IsNamespaced(gvr):
		return errors.New("manifest has no metadata.namespace")
	}
	ctx, cancel := timeoutCtx()
	defer cancel()
	var dr []string
	if dryRun {
		dr = []string{metav1.DryRunAll}
	}
	if u.GetResourceVersion() == "" {
		// not an edited live object (e.g. an assistant proposal, possibly
		// partial): server-side apply merges instead of replacing, so
		// omitted fields survive, and creates the object when missing
		_, err = ri.Apply(ctx, u.GetName(), &u, metav1.ApplyOptions{FieldManager: "kubeon", Force: true, DryRun: dr})
		return err
	}
	_, err = ri.Update(ctx, &u, metav1.UpdateOptions{FieldManager: "kubeon", DryRun: dr})
	if apierrors.IsNotFound(err) {
		u.SetResourceVersion("")
		_, err = ri.Create(ctx, &u, metav1.CreateOptions{FieldManager: "kubeon", DryRun: dr})
	}
	return err
}

// Containers returns the container names of a pod (init first).
func (c *Cluster) Containers(path string) ([]string, error) {
	o, err := c.factory.Get(client.PodGVR, path, true, labels.Everything())
	if err != nil {
		return nil, err
	}
	u, ok := o.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("unexpected %T", o)
	}
	var out []string
	for _, k := range []string{"initContainers", "containers", "ephemeralContainers"} {
		cc, _, _ := unstructured.NestedSlice(u.Object, "spec", k)
		for _, c := range cc {
			if m, ok := c.(map[string]any); ok {
				if n, ok := m["name"].(string); ok {
					out = append(out, n)
				}
			}
		}
	}
	return out, nil
}

// PodFor resolves the first pod of a controller-like resource, or returns
// path unchanged for pods.
func (c *Cluster) PodFor(gvr, path string) (string, error) {
	if gvr == client.PodGVR.String() {
		return path, nil
	}
	if ctl, ok := c.Accessor(gvr).(dao.Controller); ok {
		return ctl.Pod(path)
	}
	return "", fmt.Errorf("%s has no pods", gvr)
}

// IsLoggable reports whether logs can be tailed for the gvr.
func (c *Cluster) IsLoggable(gvr string) bool {
	_, ok := c.Accessor(gvr).(dao.Loggable)
	return ok
}

// LogLine is one log line.
type LogLine struct {
	Pod       string // set for multi-pod streams
	Container string
	Text      string
	IsError   bool
}

// LogOpts configures a log tail.
type LogOpts struct {
	Container     string
	AllContainers bool
	Previous      bool
	Timestamps    bool
	TailLines     int64
}

// TailLogs streams logs of a loggable resource until ctx is cancelled.
func (c *Cluster) TailLogs(ctx context.Context, gvr, path string, o LogOpts, out func([]LogLine)) error {
	l, ok := c.Accessor(gvr).(dao.Loggable)
	if !ok {
		return fmt.Errorf("%s has no logs", gvr)
	}
	if o.TailLines == 0 {
		o.TailLines = 500
	}
	opts := dao.LogOptions{
		Path:          path,
		Container:     o.Container,
		AllContainers: o.AllContainers,
		Previous:      o.Previous,
		ShowTimestamp: o.Timestamps,
		Lines:         o.TailLines,
		MultiPods:     gvr != client.PodGVR.String(),
	}
	cc, err := l.TailLogs(c.baseContext(ctx, client.NewGVR(gvr), "", path), &opts)
	if err != nil {
		return err
	}
	merged := make(chan *dao.LogItem, 1024)
	for _, ch := range cc {
		go func(ch dao.LogChan) {
			for it := range ch {
				select {
				case merged <- it:
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}
	go batchLogs(ctx, merged, o.Timestamps, out)

	return nil
}
