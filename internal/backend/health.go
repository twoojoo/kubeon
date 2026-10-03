package backend

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"

	"github.com/giovanni/k9desk/third_party/k9s/client"
)

// Severity of a health event.
type Severity int

const (
	SevOK Severity = iota
	SevWarn
	SevErr
)

// HealthEvent is a cluster state transition worth notifying.
type HealthEvent struct {
	Severity Severity
	Title    string
	Body     string
	GVR      string
	Path     string
	At       time.Time
}

var badWaiting = map[string]bool{
	"CrashLoopBackOff":           true,
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
	"InvalidImageName":           true,
}

// healthState tracks the last seen condition per object.
type healthState struct {
	pods  map[string]string // fqn → problem reason ("" = fine)
	nodes map[string]bool   // name → ready
	deps  map[string]bool   // fqn → available
}

// WatchHealth scans informer caches every interval and emits transitions
// (pods failing, nodes NotReady/Ready, deployments degraded/recovered).
// The first scan only seeds state. Blocks until ctx is done.
func (c *Cluster) WatchHealth(ctx context.Context, interval time.Duration, emit func([]HealthEvent)) {
	st := healthState{pods: map[string]string{}, nodes: map[string]bool{}, deps: map[string]bool{}}
	seeded := false
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		ev := c.scanHealth(&st, seeded)
		seeded = true
		if len(ev) > 0 {
			emit(ev)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c *Cluster) scanHealth(st *healthState, notify bool) []HealthEvent {
	var out []HealthEvent
	now := time.Now()
	add := func(sev Severity, gvr, path, title, body string) {
		if notify {
			out = append(out, HealthEvent{Severity: sev, Title: title, Body: body, GVR: gvr, Path: path, At: now})
		}
	}

	seen := map[string]bool{}
	for _, p := range listTyped[v1.Pod](c, client.PodGVR, client.BlankNamespace) {
		fqn := p.Namespace + "/" + p.Name
		seen[fqn] = true
		reason := podProblem(&p)
		prev, known := st.pods[fqn]
		st.pods[fqn] = reason
		if reason != "" && reason != prev && (known || notify) {
			add(SevErr, "v1/pods", fqn, "Pod "+reason, fmt.Sprintf("%s in namespace %s", p.Name, p.Namespace))
		}
		if reason == "" && prev != "" {
			add(SevOK, "v1/pods", fqn, "Pod recovered", fmt.Sprintf("%s (%s) is healthy again", p.Name, p.Namespace))
		}
	}
	for k := range st.pods {
		if !seen[k] {
			delete(st.pods, k)
		}
	}

	for _, n := range listTyped[v1.Node](c, client.NodeGVR, client.ClusterScope) {
		ready := false
		for _, cond := range n.Status.Conditions {
			if cond.Type == v1.NodeReady {
				ready = cond.Status == v1.ConditionTrue
			}
		}
		prev, known := st.nodes[n.Name]
		st.nodes[n.Name] = ready
		switch {
		case known && prev && !ready:
			add(SevErr, "v1/nodes", n.Name, "Node NotReady", n.Name+" stopped reporting Ready")
		case known && !prev && ready:
			add(SevOK, "v1/nodes", n.Name, "Node Ready", n.Name+" is Ready again")
		}
	}

	for _, d := range listTyped[appsv1.Deployment](c, client.DpGVR, client.BlankNamespace) {
		fqn := d.Namespace + "/" + d.Name
		want := int32(1)
		if d.Spec.Replicas != nil {
			want = *d.Spec.Replicas
		}
		ok := d.Status.ObservedGeneration >= d.Generation &&
			d.Status.UpdatedReplicas == want && d.Status.AvailableReplicas == want && d.Status.Replicas == want
		prev, known := st.deps[fqn]
		st.deps[fqn] = ok
		switch {
		case known && !prev && ok:
			add(SevOK, "apps/v1/deployments", fqn, "Rollout complete", fmt.Sprintf("%s: %d/%d available", fqn, want, want))
		case known && prev && !ok && d.Status.AvailableReplicas < want:
			add(SevWarn, "apps/v1/deployments", fqn, "Deployment degraded",
				fmt.Sprintf("%s: %d/%d available", fqn, d.Status.AvailableReplicas, want))
		}
	}
	return out
}

func podProblem(p *v1.Pod) string {
	if p.DeletionTimestamp != nil {
		return ""
	}
	if p.Status.Phase == v1.PodFailed {
		if p.Status.Reason != "" {
			return p.Status.Reason
		}
		return "Failed"
	}
	for _, cs := range append(append([]v1.ContainerStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...) {
		if w := cs.State.Waiting; w != nil && badWaiting[w.Reason] {
			return w.Reason
		}
		if t := cs.LastTerminationState.Terminated; t != nil && t.Reason == "OOMKilled" && cs.State.Running == nil {
			return "OOMKilled"
		}
	}
	return ""
}
