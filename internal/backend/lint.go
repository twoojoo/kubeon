package backend

import (
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/twoojoo/kubeon/third_party/k9s/client"
)

// Severity levels for lint issues reuse health Severity: SevErr, SevWarn,
// plus SevInfo for hygiene hints.
const SevInfo Severity = 10

// Issue is one sanitizer finding.
type Issue struct {
	Severity Severity
	Category string
	Rule     string
	GVR      string
	Path     string
	Message  string
}

// CategoryScore summarizes a category.
type CategoryScore struct {
	Name    string
	Scanned int
	Clean   int // resources without errors/warnings
	Errors  int
	Warns   int
	Infos   int
}

// Percent of clean resources.
func (c CategoryScore) Percent() int {
	if c.Scanned == 0 {
		return 100
	}
	return c.Clean * 100 / c.Scanned
}

// LintReport is the result of a sanitizer run.
type LintReport struct {
	Namespace  string
	Issues     []Issue
	Categories []CategoryScore
	Score      int
	At         time.Time
}

// Grade turns the score into a letter.
func (r *LintReport) Grade() string {
	switch {
	case r.Score >= 90:
		return "A"
	case r.Score >= 80:
		return "B"
	case r.Score >= 70:
		return "C"
	case r.Score >= 60:
		return "D"
	case r.Score >= 50:
		return "E"
	}
	return "F"
}

type linter struct {
	issues  []Issue
	scanned map[string]int
	dirty   map[string]map[string]bool // category → bad resource paths
}

func (l *linter) add(sev Severity, cat, rule, gvr, path, msg string, args ...any) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	l.issues = append(l.issues, Issue{Severity: sev, Category: cat, Rule: rule, GVR: gvr, Path: path, Message: msg})
	if sev == SevErr || sev == SevWarn {
		if l.dirty[cat] == nil {
			l.dirty[cat] = map[string]bool{}
		}
		l.dirty[cat][path] = true
	}
}

var systemNS = map[string]bool{"kube-system": true, "kube-public": true, "kube-node-lease": true, "local-path-storage": true}

// Lint runs the sanitizer over ns ("" = all namespaces).
func (c *Cluster) Lint(ns string) *LintReport {
	pns := ns
	if pns == "" {
		pns = client.BlankNamespace
	}
	l := &linter{scanned: map[string]int{}, dirty: map[string]map[string]bool{}}
	pods := listTyped[v1.Pod](c, client.PodGVR, pns)
	cms := listTyped[v1.ConfigMap](c, client.CmGVR, pns)
	secs := listTyped[v1.Secret](c, client.SecGVR, pns)
	svcs := listTyped[v1.Service](c, client.SvcGVR, pns)
	deps := listTyped[appsv1.Deployment](c, client.DpGVR, pns)
	stss := listTyped[appsv1.StatefulSet](c, client.StsGVR, pns)
	pvcs := listTyped[v1.PersistentVolumeClaim](c, client.PvcGVR, pns)
	ings := listTyped[netv1.Ingress](c, client.IngGVR, pns)
	nodes := listTyped[v1.Node](c, client.NodeGVR, client.ClusterScope)

	cmSet, secSet := map[string]bool{}, map[string]bool{}
	for _, x := range cms {
		cmSet[x.Namespace+"/"+x.Name] = true
	}
	for _, x := range secs {
		secSet[x.Namespace+"/"+x.Name] = true
	}
	refs := specRefs{cmSet: cmSet, secSet: secSet, cm: map[string]bool{}, sec: map[string]bool{}, pvc: map[string]bool{}}

	// ---- pods ----
	l.scanned["Pods"] = len(pods)
	for i := range pods {
		p := &pods[i]
		path := p.Namespace + "/" + p.Name
		const cat, gvr = "Pods", "v1/pods"
		if prob := podProblem(p); prob != "" {
			l.add(SevErr, cat, "POD-FAILING", gvr, path, "pod is %s", prob)
		}
		if p.Status.Phase == v1.PodPending && time.Since(p.CreationTimestamp.Time) > 5*time.Minute {
			why := ""
			for _, cd := range p.Status.Conditions {
				if cd.Type == v1.PodScheduled && cd.Status == v1.ConditionFalse {
					why = ": " + cd.Message
				}
			}
			l.add(SevWarn, cat, "POD-PENDING", gvr, path, "pending for %s%s", age(p.CreationTimestamp), why)
		}
		var restarts int32
		for _, s := range p.Status.ContainerStatuses {
			restarts += s.RestartCount
		}
		if restarts > 5 {
			l.add(SevWarn, cat, "POD-RESTARTS", gvr, path, "%d container restarts", restarts)
		}
		jobPod, naked := false, len(p.OwnerReferences) == 0
		for _, o := range p.OwnerReferences {
			if o.Kind == "Job" {
				jobPod = true
			}
		}
		if naked && !systemNS[p.Namespace] && p.Annotations["kubernetes.io/config.mirror"] == "" {
			l.add(SevInfo, cat, "POD-NAKED", gvr, path, "not managed by a controller: it won't be rescheduled")
		}
		if p.Spec.HostNetwork || p.Spec.HostPID || p.Spec.HostIPC {
			l.add(SevWarn, "Security", "SEC-HOSTNS", gvr, path, "shares host namespaces (hostNetwork/PID/IPC)")
		}
		for _, co := range p.Spec.Containers {
			cpath := path + " · " + co.Name
			if co.Resources.Requests.Cpu().IsZero() && co.Resources.Requests.Memory().IsZero() {
				l.add(SevWarn, cat, "RES-REQUESTS", gvr, path, "container %s has no resource requests", co.Name)
			}
			if co.Resources.Limits.Memory().IsZero() {
				l.add(SevInfo, cat, "RES-MEMLIMIT", gvr, path, "container %s has no memory limit", co.Name)
			}
			if !jobPod && p.Status.Phase == v1.PodRunning {
				if co.ReadinessProbe == nil {
					l.add(SevInfo, cat, "PROBE-READINESS", gvr, path, "container %s has no readiness probe", co.Name)
				}
				if co.LivenessProbe == nil {
					l.add(SevInfo, cat, "PROBE-LIVENESS", gvr, path, "container %s has no liveness probe", co.Name)
				}
			}
			img := co.Image
			if i := strings.LastIndex(img, "/"); i >= 0 {
				img = img[i+1:]
			}
			if !strings.Contains(img, ":") && !strings.Contains(co.Image, "@") || strings.HasSuffix(co.Image, ":latest") {
				l.add(SevWarn, cat, "IMG-LATEST", gvr, path, "container %s uses a floating tag (%s)", co.Name, co.Image)
			}
			if sc := co.SecurityContext; sc != nil && sc.Privileged != nil && *sc.Privileged {
				l.add(SevErr, "Security", "SEC-PRIVILEGED", gvr, path, "container %s runs privileged", co.Name)
			}
			_ = cpath
		}
		refs.scan(p.Namespace, &p.Spec, func(msg string, args ...any) {
			l.add(SevErr, "Pods", "REF-MISSING", gvr, path, msg, args...)
		})
		for _, v := range p.Spec.Volumes {
			if v.Ephemeral != nil { // generic ephemeral volume: PVC <pod>-<volume>
				refs.pvc[p.Namespace+"/"+p.Name+"-"+v.Name] = true
			}
		}
	}
	// pod templates count as usage too (scaled to 0, CronJobs between runs)
	for _, d := range deps {
		refs.scan(d.Namespace, &d.Spec.Template.Spec, nil)
	}
	for _, x := range stss {
		refs.scan(x.Namespace, &x.Spec.Template.Spec, nil)
	}
	for _, x := range listTyped[appsv1.DaemonSet](c, client.DsGVR, pns) {
		refs.scan(x.Namespace, &x.Spec.Template.Spec, nil)
	}
	for _, x := range listTyped[batchv1.CronJob](c, client.CjGVR, pns) {
		refs.scan(x.Namespace, &x.Spec.JobTemplate.Spec.Template.Spec, nil)
	}
	for _, in := range ings {
		for _, t := range in.Spec.TLS {
			refs.sec[in.Namespace+"/"+t.SecretName] = true
		}
	}
	usedCM, usedSec, usedPVC := refs.cm, refs.sec, refs.pvc

	// ---- workloads ----
	l.scanned["Workloads"] = len(deps) + len(stss)
	for _, d := range deps {
		path := d.Namespace + "/" + d.Name
		want := ptr32(d.Spec.Replicas)
		switch {
		case want > 0 && d.Status.AvailableReplicas == 0:
			l.add(SevErr, "Workloads", "DEP-UNAVAILABLE", "apps/v1/deployments", path, "no replicas available (0/%d)", want)
		case d.Status.AvailableReplicas < want:
			l.add(SevWarn, "Workloads", "DEP-DEGRADED", "apps/v1/deployments", path, "%d/%d replicas available", d.Status.AvailableReplicas, want)
		}
		if want == 1 && !systemNS[d.Namespace] {
			l.add(SevInfo, "Workloads", "DEP-SINGLE", "apps/v1/deployments", path, "single replica: no high availability")
		}
	}
	for _, s := range stss {
		path := s.Namespace + "/" + s.Name
		if want := ptr32(s.Spec.Replicas); s.Status.ReadyReplicas < want {
			l.add(SevWarn, "Workloads", "STS-DEGRADED", "apps/v1/statefulsets", path, "%d/%d replicas ready", s.Status.ReadyReplicas, want)
		}
	}

	// ---- services ----
	l.scanned["Services"] = len(svcs)
	for _, s := range svcs {
		if len(s.Spec.Selector) == 0 || s.Spec.Type == v1.ServiceTypeExternalName {
			continue
		}
		path := s.Namespace + "/" + s.Name
		sel := labels.SelectorFromSet(s.Spec.Selector)
		var matched []*v1.Pod
		for i := range pods {
			if pods[i].Namespace == s.Namespace && sel.Matches(labels.Set(pods[i].Labels)) {
				matched = append(matched, &pods[i])
			}
		}
		if len(matched) == 0 {
			l.add(SevWarn, "Services", "SVC-NOPODS", "v1/services", path, "selector %s matches no pods: no endpoints", sel)
			continue
		}
		for _, sp := range s.Spec.Ports {
			if sp.TargetPort.Type != 1 { // named ports only
				continue
			}
			name := sp.TargetPort.StrVal
			found := false
			for _, p := range matched {
				// init containers too: native sidecars (restartPolicy Always) serve ports
				for _, co := range append(append([]v1.Container{}, p.Spec.InitContainers...), p.Spec.Containers...) {
					for _, cp := range co.Ports {
						if cp.Name == name {
							found = true
						}
					}
				}
			}
			if !found {
				l.add(SevErr, "Services", "SVC-PORT", "v1/services", path, "targetPort %q is not a named port of the selected pods", name)
			}
		}
	}

	// ---- ingresses ----
	svcSet := map[string]bool{}
	for _, s := range svcs {
		svcSet[s.Namespace+"/"+s.Name] = true
	}
	l.scanned["Ingresses"] = len(ings)
	for _, in := range ings {
		path := in.Namespace + "/" + in.Name
		backends := []*netv1.IngressBackend{in.Spec.DefaultBackend}
		for _, r := range in.Spec.Rules {
			if r.HTTP != nil {
				for i := range r.HTTP.Paths {
					backends = append(backends, &r.HTTP.Paths[i].Backend)
				}
			}
		}
		for _, b := range backends {
			if b != nil && b.Service != nil && !svcSet[in.Namespace+"/"+b.Service.Name] {
				l.add(SevErr, "Ingresses", "ING-NOSVC", "networking.k8s.io/v1/ingresses", path, "backend service %s does not exist", b.Service.Name)
			}
		}
	}

	// ---- config (unused) ----
	l.scanned["Config"] = len(cms) + len(secs)
	for _, x := range cms {
		k := x.Namespace + "/" + x.Name
		if usedCM[k] || systemNS[x.Namespace] || x.Name == "kube-root-ca.crt" {
			continue
		}
		l.add(SevInfo, "Config", "CM-UNUSED", "v1/configmaps", k, "not referenced by any pod or workload")
	}
	for _, x := range secs {
		k := x.Namespace + "/" + x.Name
		if usedSec[k] || systemNS[x.Namespace] || x.Type == v1.SecretTypeServiceAccountToken ||
			strings.HasPrefix(string(x.Type), "helm.sh/") || x.Type == v1.SecretTypeTLS {
			continue
		}
		l.add(SevInfo, "Config", "SEC-UNUSED", "v1/secrets", k, "not referenced by any pod or workload")
	}

	// ---- storage ----
	l.scanned["Storage"] = len(pvcs)
	for _, pvc := range pvcs {
		k := pvc.Namespace + "/" + pvc.Name
		if pvc.Status.Phase != v1.ClaimBound {
			l.add(SevWarn, "Storage", "PVC-UNBOUND", "v1/persistentvolumeclaims", k, "claim is %s", pvc.Status.Phase)
		}
		if !usedPVC[k] {
			l.add(SevInfo, "Storage", "PVC-UNUSED", "v1/persistentvolumeclaims", k, "not mounted by any pod")
		}
	}

	// ---- nodes ----
	l.scanned["Nodes"] = len(nodes)
	for _, n := range nodes {
		for _, cd := range n.Status.Conditions {
			switch {
			case cd.Type == v1.NodeReady && cd.Status != v1.ConditionTrue:
				l.add(SevErr, "Nodes", "NODE-NOTREADY", "v1/nodes", n.Name, "node is not Ready: %s", cd.Message)
			case strings.HasSuffix(string(cd.Type), "Pressure") && cd.Status == v1.ConditionTrue:
				l.add(SevWarn, "Nodes", "NODE-PRESSURE", "v1/nodes", n.Name, "%s", cd.Type)
			}
		}
		if n.Spec.Unschedulable {
			l.add(SevInfo, "Nodes", "NODE-CORDONED", "v1/nodes", n.Name, "scheduling disabled (cordoned)")
		}
	}
	l.scanned["Security"] = len(pods)

	return l.report(ns)
}

// specRefs collects configmaps/secrets/PVCs referenced by pod specs.
type specRefs struct {
	cmSet, secSet map[string]bool // existing objects
	cm, sec, pvc  map[string]bool // referenced
}

// scan marks the references of spec; missing (non-optional) ones are
// reported through missing when it is not nil.
func (r *specRefs) scan(ns string, spec *v1.PodSpec, missing func(msg string, args ...any)) {
	isOpt := func(o *bool) bool { return o != nil && *o }
	cm := func(name string, optional *bool, what string) {
		k := ns + "/" + name
		r.cm[k] = true
		if missing != nil && !r.cmSet[k] && !isOpt(optional) {
			missing("%s references missing configmap %s", what, name)
		}
	}
	sec := func(name string, optional *bool, what string) {
		k := ns + "/" + name
		r.sec[k] = true
		if missing != nil && !r.secSet[k] && !isOpt(optional) {
			missing("%s references missing secret %s", what, name)
		}
	}
	for _, v := range spec.Volumes {
		what := "volume " + v.Name
		switch {
		case v.ConfigMap != nil:
			cm(v.ConfigMap.Name, v.ConfigMap.Optional, what)
		case v.Secret != nil:
			sec(v.Secret.SecretName, v.Secret.Optional, what)
		case v.PersistentVolumeClaim != nil:
			r.pvc[ns+"/"+v.PersistentVolumeClaim.ClaimName] = true
		case v.Projected != nil:
			for _, src := range v.Projected.Sources {
				if src.ConfigMap != nil {
					cm(src.ConfigMap.Name, src.ConfigMap.Optional, what)
				}
				if src.Secret != nil {
					sec(src.Secret.Name, src.Secret.Optional, what)
				}
			}
		}
	}
	for _, s := range spec.ImagePullSecrets {
		r.sec[ns+"/"+s.Name] = true
	}
	for _, co := range append(append([]v1.Container{}, spec.InitContainers...), spec.Containers...) {
		for _, e := range co.Env {
			if e.ValueFrom == nil {
				continue
			}
			if ref := e.ValueFrom.ConfigMapKeyRef; ref != nil {
				cm(ref.Name, ref.Optional, "env "+e.Name)
			}
			if ref := e.ValueFrom.SecretKeyRef; ref != nil {
				sec(ref.Name, ref.Optional, "env "+e.Name)
			}
		}
		for _, ef := range co.EnvFrom {
			if ef.ConfigMapRef != nil {
				cm(ef.ConfigMapRef.Name, ef.ConfigMapRef.Optional, "envFrom of "+co.Name)
			}
			if ef.SecretRef != nil {
				sec(ef.SecretRef.Name, ef.SecretRef.Optional, "envFrom of "+co.Name)
			}
		}
	}
}

func (l *linter) report(ns string) *LintReport {
	r := &LintReport{Namespace: ns, Issues: l.issues, At: time.Now()}
	order := []string{"Pods", "Workloads", "Services", "Ingresses", "Config", "Storage", "Nodes", "Security"}
	var total, clean int
	for _, name := range order {
		cs := CategoryScore{Name: name, Scanned: l.scanned[name]}
		cs.Clean = max(0, cs.Scanned-len(l.dirty[name]))
		for _, is := range l.issues {
			if is.Category != name {
				continue
			}
			switch is.Severity {
			case SevErr:
				cs.Errors++
			case SevWarn:
				cs.Warns++
			default:
				cs.Infos++
			}
		}
		total += cs.Scanned
		clean += cs.Clean
		r.Categories = append(r.Categories, cs)
	}
	r.Score = 100
	if total > 0 {
		r.Score = clean * 100 / total
	}
	sort.SliceStable(r.Issues, func(i, j int) bool {
		si, sj := sevRank(r.Issues[i].Severity), sevRank(r.Issues[j].Severity)
		if si != sj {
			return si < sj
		}
		if r.Issues[i].Category != r.Issues[j].Category {
			return r.Issues[i].Category < r.Issues[j].Category
		}
		return r.Issues[i].Path < r.Issues[j].Path
	})
	return r
}

func sevRank(s Severity) int {
	switch s {
	case SevErr:
		return 0
	case SevWarn:
		return 1
	}
	return 2
}
