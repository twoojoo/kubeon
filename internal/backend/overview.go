package backend

import (
	"sort"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/twoojoo/kubeon/third_party/k9s/client"
)

// Overview is the cluster dashboard summary (from k9s informer caches).
type Overview struct {
	Nodes, NodesReady int
	Namespaces        int
	PodPhases         map[string]int
	Pods              int
	Deployments       int
	DeploymentsBad    int
	CPUCapacity       int64 // millicores (allocatable)
	CPURequests       int64
	CPUUsage          int64 // -1 when metrics unavailable
	MemCapacity       int64 // bytes (allocatable)
	MemRequests       int64
	MemUsage          int64
	Warnings          []EventInfo
}

// EventInfo is a warning event.
type EventInfo struct {
	Namespace, Object, Reason, Message string
	Count                              int32
	Last                               time.Time
}

func listTyped[T any](c *Cluster, gvr *client.GVR, ns string) []T {
	oo, err := c.factory.List(gvr, ns, true, labels.Everything())
	if err != nil {
		return nil
	}
	out := make([]T, 0, len(oo))
	for _, o := range oo {
		u, ok := o.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		var t T
		if runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &t) == nil {
			out = append(out, t)
		}
	}
	return out
}

// Overview computes the dashboard for namespace ns ("" = all).
func (c *Cluster) Overview(ns string) Overview {
	o := Overview{PodPhases: map[string]int{}, CPUUsage: -1, MemUsage: -1}
	pns := ns
	if pns == "" {
		pns = client.BlankNamespace
	}
	for _, n := range listTyped[v1.Node](c, client.NodeGVR, client.ClusterScope) {
		o.Nodes++
		for _, cond := range n.Status.Conditions {
			if cond.Type == v1.NodeReady && cond.Status == v1.ConditionTrue {
				o.NodesReady++
			}
		}
		o.CPUCapacity += n.Status.Allocatable.Cpu().MilliValue()
		o.MemCapacity += n.Status.Allocatable.Memory().Value()
	}
	o.Namespaces = len(listTyped[v1.Namespace](c, client.NsGVR, client.ClusterScope))
	for _, p := range listTyped[v1.Pod](c, client.PodGVR, pns) {
		o.Pods++
		o.PodPhases[podPhase(&p)]++
		if p.Status.Phase == v1.PodSucceeded || p.Status.Phase == v1.PodFailed {
			continue
		}
		for _, co := range p.Spec.Containers {
			if q, ok := co.Resources.Requests[v1.ResourceCPU]; ok {
				o.CPURequests += q.MilliValue()
			}
			if q, ok := co.Resources.Requests[v1.ResourceMemory]; ok {
				o.MemRequests += q.Value()
			}
		}
	}
	for _, d := range listTyped[unstructured.Unstructured](c, client.DpGVR, pns) {
		o.Deployments++
		want, _, _ := unstructured.NestedInt64(d.Object, "spec", "replicas")
		avail, _, _ := unstructured.NestedInt64(d.Object, "status", "availableReplicas")
		if avail < want {
			o.DeploymentsBad++
		}
	}
	for _, e := range listTyped[v1.Event](c, client.NewGVR("v1/events"), pns) {
		if e.Type != v1.EventTypeWarning {
			continue
		}
		last := e.LastTimestamp.Time
		if last.IsZero() {
			last = e.EventTime.Time
		}
		if last.IsZero() {
			last = e.CreationTimestamp.Time
		}
		o.Warnings = append(o.Warnings, EventInfo{
			Namespace: e.Namespace,
			Object:    e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
			Reason:    e.Reason,
			Message:   e.Message,
			Count:     e.Count,
			Last:      last,
		})
	}
	sort.Slice(o.Warnings, func(i, j int) bool { return o.Warnings[i].Last.After(o.Warnings[j].Last) })
	if len(o.Warnings) > 200 {
		o.Warnings = o.Warnings[:200]
	}
	if c.conn.HasMetrics() {
		if mx, err := c.conn.MXDial(); err == nil {
			ctx, cancel := timeoutCtx()
			defer cancel()
			if nm, err := mx.MetricsV1beta1().NodeMetricses().List(ctx, metaListAll); err == nil {
				var cpu, mem resource.Quantity
				for _, m := range nm.Items {
					cpu.Add(*m.Usage.Cpu())
					mem.Add(*m.Usage.Memory())
				}
				o.CPUUsage, o.MemUsage = cpu.MilliValue(), mem.Value()
			}
		}
	}
	return o
}

// podPhase mirrors kubectl's STATUS (CrashLoopBackOff etc. count as their reason).
func podPhase(p *v1.Pod) string {
	if p.DeletionTimestamp != nil {
		return "Terminating"
	}
	for _, cs := range p.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil && w.Reason != "" && w.Reason != "ContainerCreating" {
			return "Failing"
		}
	}
	switch p.Status.Phase {
	case v1.PodRunning:
		return "Running"
	case v1.PodSucceeded:
		return "Succeeded"
	case v1.PodFailed:
		return "Failed"
	default:
		return "Pending"
	}
}

var metaListAll = metav1.ListOptions{}
