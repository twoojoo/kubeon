package backend

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"

	"github.com/giovanni/kubeon/third_party/k9s/client"
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
	deps  map[string]depState
}

type depState struct{ complete, healthy bool }

// WatchHealth scans informer caches every interval and emits transitions
// (pods failing, nodes NotReady/Ready, deployments degraded/recovered).
// The first scan only seeds state. Blocks until ctx is done.
func (c *Cluster) WatchHealth(ctx context.Context, interval time.Duration, emit func([]HealthEvent)) {
	st := healthState{pods: map[string]string{}, nodes: map[string]bool{}, deps: map[string]depState{}}
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
		if p.DeletionTimestamp != nil {
			continue // terminating: keep the last state, never "recovered"
		}
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

	seen = map[string]bool{}
	for _, n := range listTyped[v1.Node](c, client.NodeGVR, client.ClusterScope) {
		seen[n.Name] = true
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
	for k := range st.nodes {
		if !seen[k] {
			delete(st.nodes, k)
		}
	}

	seen = map[string]bool{}
	for _, d := range listTyped[appsv1.Deployment](c, client.DpGVR, client.BlankNamespace) {
		fqn := d.Namespace + "/" + d.Name
		seen[fqn] = true
		want := int32(1)
		if d.Spec.Replicas != nil {
			want = *d.Spec.Replicas
		}
		complete := d.Status.ObservedGeneration >= d.Generation &&
			d.Status.UpdatedReplicas == want && d.Status.AvailableReplicas == want && d.Status.Replicas == want
		// degraded = the controller says so (below minimum availability or
		// rollout stuck), not a scale-up or a rolling update in progress
		healthy := true
		for _, cond := range d.Status.Conditions {
			switch {
			case cond.Type == appsv1.DeploymentAvailable && cond.Status == v1.ConditionFalse,
				cond.Type == appsv1.DeploymentProgressing && cond.Status == v1.ConditionFalse:
				healthy = false
			}
		}
		prev, known := st.deps[fqn]
		st.deps[fqn] = depState{complete: complete, healthy: healthy}
		switch {
		case !known:
		case prev.healthy && !healthy:
			add(SevWarn, "apps/v1/deployments", fqn, "Deployment degraded",
				fmt.Sprintf("%s: %d/%d available", fqn, d.Status.AvailableReplicas, want))
		case !prev.complete && complete:
			add(SevOK, "apps/v1/deployments", fqn, "Rollout complete", fmt.Sprintf("%s: %d/%d available", fqn, want, want))
		}
	}
	for k := range st.deps {
		if !seen[k] {
			delete(st.deps, k)
		}
	}
	return out
}

// podProblem names what is wrong with a pod ("" = fine). Image pull errors
// are reported as ImagePullBackOff and a crashing container as
// CrashLoopBackOff for its whole restart cycle (also while briefly Running or
// Terminated between back-offs), so the state doesn't flap.
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
	if p.Status.Phase == v1.PodSucceeded {
		return ""
	}
	for _, cs := range append(append([]v1.ContainerStatus{}, p.Status.InitContainerStatuses...), p.Status.ContainerStatuses...) {
		if w := cs.State.Waiting; w != nil && badWaiting[w.Reason] {
			if w.Reason == "ErrImagePull" {
				return "ImagePullBackOff"
			}
			return w.Reason
		}
		last := cs.LastTerminationState.Terminated
		if last == nil || cs.Ready {
			continue
		}
		if last.Reason == "OOMKilled" {
			return "OOMKilled"
		}
		// crashed before and not back to ready yet
		if last.ExitCode != 0 && cs.RestartCount > 0 {
			if t := cs.State.Terminated; t != nil && t.ExitCode == 0 {
				continue
			}
			return "CrashLoopBackOff"
		}
	}
	return ""
}
