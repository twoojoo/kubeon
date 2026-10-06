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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"

	"github.com/giovanni/kubeon/third_party/k9s/client"
)

// Link points to another resource.
type Link struct {
	GVR  string
	Path string
}

// Tone colors a value.
type Tone uint8

const (
	ToneNone Tone = iota
	ToneOK
	ToneWarn
	ToneErr
	ToneDim
)

// Field is a key/value with optional link and tone.
type Field struct {
	Key   string
	Value string
	Link  *Link
	Tone  Tone
	Mono  bool
}

// DTable is a small table inside a section.
type DTable struct {
	Columns []string
	Rows    [][]Field
}

// Section is a titled group of details.
type Section struct {
	Title  string
	Icon   string // kind hint for the UI ("container", "volume", …)
	Badge  Field  // optional header badge
	Fields []Field
	Chips  []Field
	Table  *DTable
	Text   string // free text (e.g. probe commands)
	Cards  []Section
}

// Details is the structured view of one resource.
type Details struct {
	GVR      string
	Path     string
	Sections []Section
}

func age(t metav1.Time) string {
	if t.IsZero() {
		return "-"
	}
	return duration.HumanDuration(time.Since(t.Time))
}

func toTyped(u *unstructured.Unstructured, out any) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, out)
}

func (c *Cluster) getU(gvr, path string) (*unstructured.Unstructured, error) {
	if !IsNamespaced(gvr) && !strings.Contains(path, "/") {
		path = client.ClusterScope + "/" + path // k9s convention for cluster-scoped
	}
	o, err := c.factory.Get(client.NewGVR(gvr), path, true, labels.Everything())
	if err != nil {
		return nil, err
	}
	u, ok := o.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("unexpected %T", o)
	}
	return u, nil
}

func nsPath(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "/" + name
}

// ownerGVR maps an owner reference to a gvr (best effort).
func ownerGVR(o metav1.OwnerReference) string {
	gv, _ := strings.CutSuffix(o.APIVersion, "/")
	if g, ok := LookupAlias(strings.ToLower(o.Kind)); ok {
		return g
	}
	return gv
}

// Details builds the structured view of a resource.
func (c *Cluster) Details(gvr, path string) (*Details, error) {
	u, err := c.getU(gvr, path)
	if err != nil {
		return nil, err
	}
	d := &Details{GVR: gvr, Path: path}
	d.Sections = append(d.Sections, metaSection(u))
	switch gvr {
	case "v1/pods":
		var p v1.Pod
		if toTyped(u, &p) == nil {
			d.Sections = append(d.Sections, c.podSections(&p)...)
		}
	case "apps/v1/deployments":
		var o appsv1.Deployment
		if toTyped(u, &o) == nil {
			d.Sections = append(d.Sections, c.workloadSections(o.Namespace, o.Spec.Selector, &o.Spec.Template,
				[]Field{
					{Key: "Replicas", Value: fmt.Sprintf("%d desired · %d updated · %d ready · %d available", ptr32(o.Spec.Replicas), o.Status.UpdatedReplicas, o.Status.ReadyReplicas, o.Status.AvailableReplicas), Tone: replicaTone(ptr32(o.Spec.Replicas), o.Status.AvailableReplicas)},
					{Key: "Strategy", Value: string(o.Spec.Strategy.Type)},
					{Key: "Revision", Value: o.Annotations["deployment.kubernetes.io/revision"]},
				})...)
		}
	case "apps/v1/statefulsets":
		var o appsv1.StatefulSet
		if toTyped(u, &o) == nil {
			d.Sections = append(d.Sections, c.workloadSections(o.Namespace, o.Spec.Selector, &o.Spec.Template,
				[]Field{
					{Key: "Replicas", Value: fmt.Sprintf("%d desired · %d ready · %d current", ptr32(o.Spec.Replicas), o.Status.ReadyReplicas, o.Status.CurrentReplicas), Tone: replicaTone(ptr32(o.Spec.Replicas), o.Status.ReadyReplicas)},
					{Key: "Service", Value: o.Spec.ServiceName, Link: linkIf("v1/services", o.Namespace, o.Spec.ServiceName)},
					{Key: "Update strategy", Value: string(o.Spec.UpdateStrategy.Type)},
				})...)
		}
	case "apps/v1/daemonsets":
		var o appsv1.DaemonSet
		if toTyped(u, &o) == nil {
			d.Sections = append(d.Sections, c.workloadSections(o.Namespace, o.Spec.Selector, &o.Spec.Template,
				[]Field{{Key: "Scheduled", Value: fmt.Sprintf("%d desired · %d ready · %d available", o.Status.DesiredNumberScheduled, o.Status.NumberReady, o.Status.NumberAvailable), Tone: replicaTone(o.Status.DesiredNumberScheduled, o.Status.NumberAvailable)}})...)
		}
	case "apps/v1/replicasets":
		var o appsv1.ReplicaSet
		if toTyped(u, &o) == nil {
			d.Sections = append(d.Sections, c.workloadSections(o.Namespace, o.Spec.Selector, &o.Spec.Template,
				[]Field{{Key: "Replicas", Value: fmt.Sprintf("%d desired · %d ready", ptr32(o.Spec.Replicas), o.Status.ReadyReplicas), Tone: replicaTone(ptr32(o.Spec.Replicas), o.Status.ReadyReplicas)}})...)
		}
	case "batch/v1/jobs":
		var o batchv1.Job
		if toTyped(u, &o) == nil {
			f := []Field{{Key: "Status", Value: fmt.Sprintf("%d active · %d succeeded · %d failed", o.Status.Active, o.Status.Succeeded, o.Status.Failed)}}
			if o.Spec.Completions != nil {
				f = append(f, Field{Key: "Completions", Value: fmt.Sprint(*o.Spec.Completions)})
			}
			d.Sections = append(d.Sections, c.workloadSections(o.Namespace, o.Spec.Selector, &o.Spec.Template, f)...)
		}
	case "batch/v1/cronjobs":
		var o batchv1.CronJob
		if toTyped(u, &o) == nil {
			last := "-"
			if o.Status.LastScheduleTime != nil {
				last = age(*o.Status.LastScheduleTime) + " ago"
			}
			susp := "no"
			if o.Spec.Suspend != nil && *o.Spec.Suspend {
				susp = "yes"
			}
			d.Sections = append(d.Sections, Section{Title: "Schedule", Fields: []Field{
				{Key: "Schedule", Value: o.Spec.Schedule, Mono: true},
				{Key: "Last schedule", Value: last},
				{Key: "Active jobs", Value: fmt.Sprint(len(o.Status.Active))},
				{Key: "Suspended", Value: susp},
			}})
			d.Sections = append(d.Sections, containerTemplate(o.Namespace, &o.Spec.JobTemplate.Spec.Template))
		}
	case "v1/services":
		var s v1.Service
		if toTyped(u, &s) == nil {
			d.Sections = append(d.Sections, c.serviceSections(&s)...)
		}
	case "networking.k8s.io/v1/ingresses":
		var in netv1.Ingress
		if toTyped(u, &in) == nil {
			d.Sections = append(d.Sections, ingressSection(&in))
		}
	case "v1/nodes":
		var n v1.Node
		if toTyped(u, &n) == nil {
			d.Sections = append(d.Sections, c.nodeSections(&n)...)
		}
	case "v1/configmaps":
		var cm v1.ConfigMap
		if toTyped(u, &cm) == nil {
			t := &DTable{Columns: []string{"KEY", "SIZE"}}
			for _, k := range sortedKeys(cm.Data) {
				t.Rows = append(t.Rows, []Field{{Value: k, Mono: true}, {Value: fmt.Sprintf("%d B", len(cm.Data[k]))}})
			}
			d.Sections = append(d.Sections, Section{Title: "Data", Table: t}, c.usedBy(cm.Namespace, "configmap", cm.Name))
		}
	case "v1/secrets":
		var s v1.Secret
		if toTyped(u, &s) == nil {
			t := &DTable{Columns: []string{"KEY", "SIZE"}}
			keys := make([]string, 0, len(s.Data))
			for k := range s.Data {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				t.Rows = append(t.Rows, []Field{{Value: k, Mono: true}, {Value: fmt.Sprintf("%d B", len(s.Data[k]))}})
			}
			d.Sections = append(d.Sections, Section{Title: "Data (values in the Data tab)", Fields: []Field{{Key: "Type", Value: string(s.Type)}}, Table: t},
				c.usedBy(s.Namespace, "secret", s.Name))
		}
	case "v1/persistentvolumeclaims":
		var pvc v1.PersistentVolumeClaim
		if toTyped(u, &pvc) == nil {
			capStr := "—"
			if q, ok := pvc.Status.Capacity[v1.ResourceStorage]; ok && !q.IsZero() {
				capStr = HumanQuantity(v1.ResourceStorage, q)
			} else if q, ok := pvc.Spec.Resources.Requests[v1.ResourceStorage]; ok {
				capStr = HumanQuantity(v1.ResourceStorage, q) + " (requested)"
			}
			sc := ""
			if pvc.Spec.StorageClassName != nil {
				sc = *pvc.Spec.StorageClassName
			}
			tone := ToneOK
			if pvc.Status.Phase != v1.ClaimBound {
				tone = ToneWarn
			}
			d.Sections = append(d.Sections, Section{Title: "Claim", Fields: []Field{
				{Key: "Status", Value: string(pvc.Status.Phase), Tone: tone},
				{Key: "Capacity", Value: capStr},
				{Key: "Storage class", Value: sc, Link: linkIf("storage.k8s.io/v1/storageclasses", "", sc)},
				{Key: "Volume", Value: pvc.Spec.VolumeName, Link: linkIf("v1/persistentvolumes", "", pvc.Spec.VolumeName)},
				{Key: "Access modes", Value: accessModes(pvc.Spec.AccessModes)},
			}}, c.usedBy(pvc.Namespace, "pvc", pvc.Name))
		}
	}
	if cond := conditionsSection(u); cond != nil {
		d.Sections = append(d.Sections, *cond)
	}
	if ev := c.eventsSection(u); ev != nil {
		d.Sections = append(d.Sections, *ev)
	}
	return d, nil
}

func ptr32(p *int32) int32 {
	if p == nil {
		return 1
	}
	return *p
}

func replicaTone(want, have int32) Tone {
	switch {
	case have >= want:
		return ToneOK
	case have == 0:
		return ToneErr
	default:
		return ToneWarn
	}
}

func linkIf(gvr, ns, name string) *Link {
	if name == "" {
		return nil
	}
	return &Link{GVR: gvr, Path: nsPath(ns, name)}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func accessModes(mm []v1.PersistentVolumeAccessMode) string {
	ss := make([]string, len(mm))
	for i, m := range mm {
		ss[i] = string(m)
	}
	return strings.Join(ss, ", ")
}

func metaSection(u *unstructured.Unstructured) Section {
	s := Section{Title: "Metadata", Fields: []Field{
		{Key: "Name", Value: u.GetName(), Mono: true},
	}}
	if ns := u.GetNamespace(); ns != "" {
		s.Fields = append(s.Fields, Field{Key: "Namespace", Value: ns, Link: &Link{GVR: "v1/namespaces", Path: ns}})
	}
	s.Fields = append(s.Fields, Field{Key: "Created", Value: age(u.GetCreationTimestamp()) + " ago (" + u.GetCreationTimestamp().Format("2006-01-02 15:04") + ")"})
	for _, o := range u.GetOwnerReferences() {
		s.Fields = append(s.Fields, Field{Key: "Controlled by", Value: o.Kind + " " + o.Name, Link: &Link{GVR: ownerGVR(o), Path: nsPath(u.GetNamespace(), o.Name)}})
	}
	if f := u.GetFinalizers(); len(f) > 0 {
		s.Fields = append(s.Fields, Field{Key: "Finalizers", Value: strings.Join(f, ", "), Mono: true})
	}
	for _, k := range sortedKeys(u.GetLabels()) {
		s.Chips = append(s.Chips, Field{Key: k, Value: u.GetLabels()[k]})
	}
	ann := u.GetAnnotations()
	keys := sortedKeys(ann)
	var shown []string
	for _, k := range keys {
		if k == "kubectl.kubernetes.io/last-applied-configuration" {
			continue
		}
		shown = append(shown, k)
	}
	if len(shown) > 0 {
		t := &DTable{Columns: []string{"ANNOTATION", "VALUE"}}
		for _, k := range shown {
			v := ann[k]
			if len(v) > 160 {
				v = v[:160] + "…"
			}
			t.Rows = append(t.Rows, []Field{{Value: k, Mono: true, Tone: ToneDim}, {Value: v, Mono: true}})
		}
		s.Table = t
	}
	return s
}

func conditionsSection(u *unstructured.Unstructured) *Section {
	cc, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	if len(cc) == 0 {
		return nil
	}
	t := &DTable{Columns: []string{"TYPE", "STATUS", "REASON", "AGE", "MESSAGE"}}
	for _, c := range cc {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		typ, st := fmt.Sprint(m["type"]), fmt.Sprint(m["status"])
		tone := ToneOK
		bad := (st == "False" && !strings.Contains(typ, "Pressure") && typ != "NetworkUnavailable") ||
			(st == "True" && (strings.Contains(typ, "Pressure") || typ == "NetworkUnavailable" || typ == "Failed"))
		if bad {
			tone = ToneErr
		}
		if st == "Unknown" {
			tone = ToneWarn
		}
		ago := "-"
		if s, ok := m["lastTransitionTime"].(string); ok {
			if tt, err := time.Parse(time.RFC3339, s); err == nil {
				ago = duration.HumanDuration(time.Since(tt))
			}
		}
		t.Rows = append(t.Rows, []Field{{Value: typ}, {Value: st, Tone: tone}, {Value: str(m["reason"])}, {Value: ago, Tone: ToneDim}, {Value: str(m["message"])}})
	}
	return &Section{Title: "Conditions", Table: t}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func (c *Cluster) eventsSection(u *unstructured.Unstructured) *Section {
	// Events matches names by substring: filter the exact object before
	// limiting, or events of children (web-xxx pods) crowd it out
	ev := c.Events(u.GetNamespace(), u.GetName(), false, 0)
	var rows [][]Field
	for _, e := range ev {
		if e.Object != u.GetKind()+"/"+u.GetName() {
			continue
		}
		if len(rows) == 15 {
			break
		}
		tone := ToneNone
		if e.Type == "Warning" {
			tone = ToneWarn
		}
		rows = append(rows, []Field{{Value: duration.HumanDuration(time.Since(e.Last)), Tone: ToneDim}, {Value: e.Reason, Tone: tone}, {Value: fmt.Sprintf("x%d", e.Count), Tone: ToneDim}, {Value: e.Message}})
	}
	if len(rows) == 0 {
		return nil
	}
	return &Section{Title: "Events", Table: &DTable{Columns: []string{"AGE", "REASON", "COUNT", "MESSAGE"}, Rows: rows}}
}

func resourcesString(r v1.ResourceRequirements) (req, lim string) {
	f := func(l v1.ResourceList) string {
		var parts []string
		if q, ok := l[v1.ResourceCPU]; ok {
			parts = append(parts, "cpu "+HumanQuantity(v1.ResourceCPU, q))
		}
		if q, ok := l[v1.ResourceMemory]; ok {
			parts = append(parts, "mem "+HumanQuantity(v1.ResourceMemory, q))
		}
		if len(parts) == 0 {
			return "none"
		}
		return strings.Join(parts, " · ")
	}
	return f(r.Requests), f(r.Limits)
}

func probeString(p *v1.Probe) string {
	if p == nil {
		return "none"
	}
	var what string
	switch {
	case p.HTTPGet != nil:
		what = fmt.Sprintf("http GET :%s%s", p.HTTPGet.Port.String(), p.HTTPGet.Path)
	case p.TCPSocket != nil:
		what = "tcp :" + p.TCPSocket.Port.String()
	case p.Exec != nil:
		what = "exec " + strings.Join(p.Exec.Command, " ")
	case p.GRPC != nil:
		what = fmt.Sprintf("grpc :%d", p.GRPC.Port)
	}
	return fmt.Sprintf("%s (delay %ds, every %ds)", what, p.InitialDelaySeconds, p.PeriodSeconds)
}

// containerCard describes one container (spec + optional status).
func containerCard(ns string, co v1.Container, st *v1.ContainerStatus, init bool) Section {
	title := co.Name
	if init {
		title += " (init)"
	}
	card := Section{Title: title, Icon: "container"}
	card.Fields = append(card.Fields, Field{Key: "Image", Value: co.Image, Mono: true})
	if st != nil {
		state, tone := "unknown", ToneWarn
		switch {
		case st.State.Running != nil:
			state, tone = "running since "+age(st.State.Running.StartedAt), ToneOK
		case st.State.Waiting != nil:
			state = "waiting: " + st.State.Waiting.Reason
			if st.State.Waiting.Message != "" {
				state += " — " + st.State.Waiting.Message
			}
			tone = ToneErr
			if st.State.Waiting.Reason == "ContainerCreating" || st.State.Waiting.Reason == "PodInitializing" {
				tone = ToneWarn
			}
		case st.State.Terminated != nil:
			t := st.State.Terminated
			state = fmt.Sprintf("terminated: %s (exit %d)", t.Reason, t.ExitCode)
			tone = ToneDim
			if t.ExitCode != 0 {
				tone = ToneErr
			}
		}
		card.Badge = Field{Value: state, Tone: tone}
		ready := "no"
		if st.Ready {
			ready = "yes"
		}
		card.Fields = append(card.Fields, Field{Key: "Ready", Value: ready, Tone: map[bool]Tone{true: ToneOK, false: ToneWarn}[st.Ready]})
		rt := ToneNone
		if st.RestartCount > 3 {
			rt = ToneWarn
		}
		card.Fields = append(card.Fields, Field{Key: "Restarts", Value: fmt.Sprint(st.RestartCount), Tone: rt})
		if lt := st.LastTerminationState.Terminated; lt != nil {
			card.Fields = append(card.Fields, Field{Key: "Last termination", Value: fmt.Sprintf("%s (exit %d) %s ago", lt.Reason, lt.ExitCode, age(lt.FinishedAt)), Tone: ToneWarn})
		}
	}
	if len(co.Command) > 0 || len(co.Args) > 0 {
		card.Fields = append(card.Fields, Field{Key: "Command", Value: strings.Join(append(append([]string{}, co.Command...), co.Args...), " "), Mono: true})
	}
	var ports []string
	for _, p := range co.Ports {
		s := fmt.Sprintf("%d/%s", p.ContainerPort, p.Protocol)
		if p.Name != "" {
			s = p.Name + " " + s
		}
		ports = append(ports, s)
	}
	if len(ports) > 0 {
		card.Fields = append(card.Fields, Field{Key: "Ports", Value: strings.Join(ports, ", "), Mono: true})
	}
	req, lim := resourcesString(co.Resources)
	card.Fields = append(card.Fields,
		Field{Key: "Requests", Value: req, Tone: map[bool]Tone{true: ToneWarn, false: ToneNone}[req == "none"]},
		Field{Key: "Limits", Value: lim, Tone: map[bool]Tone{true: ToneWarn, false: ToneNone}[lim == "none"]},
	)
	if !init {
		card.Fields = append(card.Fields,
			Field{Key: "Liveness", Value: probeString(co.LivenessProbe), Tone: map[bool]Tone{true: ToneDim, false: ToneNone}[co.LivenessProbe == nil]},
			Field{Key: "Readiness", Value: probeString(co.ReadinessProbe), Tone: map[bool]Tone{true: ToneDim, false: ToneNone}[co.ReadinessProbe == nil]},
		)
	}
	if len(co.Env) > 0 || len(co.EnvFrom) > 0 {
		t := &DTable{Columns: []string{"ENV", "VALUE"}}
		for _, e := range co.Env {
			f := Field{Value: e.Value, Mono: true}
			switch {
			case e.ValueFrom == nil:
				if sensitiveEnv(e.Name) {
					f.Value, f.Tone = "<redacted>", ToneDim
				}
			case e.ValueFrom.SecretKeyRef != nil:
				r := e.ValueFrom.SecretKeyRef
				f = Field{Value: "secret " + r.Name + " · " + r.Key, Link: &Link{GVR: "v1/secrets", Path: nsPath(ns, r.Name)}}
			case e.ValueFrom.ConfigMapKeyRef != nil:
				r := e.ValueFrom.ConfigMapKeyRef
				f = Field{Value: "configmap " + r.Name + " · " + r.Key, Link: &Link{GVR: "v1/configmaps", Path: nsPath(ns, r.Name)}}
			case e.ValueFrom.FieldRef != nil:
				f = Field{Value: "field " + e.ValueFrom.FieldRef.FieldPath, Tone: ToneDim}
			case e.ValueFrom.ResourceFieldRef != nil:
				f = Field{Value: "resource " + e.ValueFrom.ResourceFieldRef.Resource, Tone: ToneDim}
			}
			t.Rows = append(t.Rows, []Field{{Value: e.Name, Mono: true}, f})
		}
		for _, ef := range co.EnvFrom {
			switch {
			case ef.SecretRef != nil:
				t.Rows = append(t.Rows, []Field{{Value: ef.Prefix + "*", Mono: true}, {Value: "all keys of secret " + ef.SecretRef.Name, Link: &Link{GVR: "v1/secrets", Path: nsPath(ns, ef.SecretRef.Name)}}})
			case ef.ConfigMapRef != nil:
				t.Rows = append(t.Rows, []Field{{Value: ef.Prefix + "*", Mono: true}, {Value: "all keys of configmap " + ef.ConfigMapRef.Name, Link: &Link{GVR: "v1/configmaps", Path: nsPath(ns, ef.ConfigMapRef.Name)}}})
			}
		}
		card.Table = t
	}
	if len(co.VolumeMounts) > 0 {
		var mm []string
		for _, m := range co.VolumeMounts {
			s := m.Name + " → " + m.MountPath
			if m.ReadOnly {
				s += " (ro)"
			}
			mm = append(mm, s)
		}
		card.Text = "Mounts:\n" + strings.Join(mm, "\n")
	}
	return card
}

func sensitiveEnv(name string) bool {
	n := strings.ToLower(name)
	for _, s := range []string{"pass", "secret", "token", "key", "credential", "auth"} {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

func volumesSection(ns string, vols []v1.Volume) *Section {
	if len(vols) == 0 {
		return nil
	}
	t := &DTable{Columns: []string{"VOLUME", "SOURCE"}}
	for _, v := range vols {
		f := Field{Value: "other", Tone: ToneDim}
		switch {
		case v.ConfigMap != nil:
			f = Field{Value: "configmap " + v.ConfigMap.Name, Link: &Link{GVR: "v1/configmaps", Path: nsPath(ns, v.ConfigMap.Name)}}
		case v.Secret != nil:
			f = Field{Value: "secret " + v.Secret.SecretName, Link: &Link{GVR: "v1/secrets", Path: nsPath(ns, v.Secret.SecretName)}}
		case v.PersistentVolumeClaim != nil:
			f = Field{Value: "pvc " + v.PersistentVolumeClaim.ClaimName, Link: &Link{GVR: "v1/persistentvolumeclaims", Path: nsPath(ns, v.PersistentVolumeClaim.ClaimName)}}
		case v.EmptyDir != nil:
			f = Field{Value: "emptyDir", Tone: ToneDim}
		case v.HostPath != nil:
			f = Field{Value: "hostPath " + v.HostPath.Path, Tone: ToneWarn}
		case v.Projected != nil:
			f = Field{Value: "projected", Tone: ToneDim}
		case v.DownwardAPI != nil:
			f = Field{Value: "downwardAPI", Tone: ToneDim}
		}
		t.Rows = append(t.Rows, []Field{{Value: v.Name, Mono: true}, f})
	}
	return &Section{Title: "Volumes", Table: t}
}

func (c *Cluster) podSections(p *v1.Pod) []Section {
	phaseTone := ToneOK
	switch p.Status.Phase {
	case v1.PodPending:
		phaseTone = ToneWarn
	case v1.PodFailed:
		phaseTone = ToneErr
	case v1.PodSucceeded:
		phaseTone = ToneDim
	}
	status := Section{Title: "Status", Fields: []Field{
		{Key: "Phase", Value: string(p.Status.Phase), Tone: phaseTone},
		{Key: "Node", Value: p.Spec.NodeName, Link: linkIf("v1/nodes", "", p.Spec.NodeName)},
		{Key: "Pod IP", Value: p.Status.PodIP, Mono: true},
		{Key: "QoS class", Value: string(p.Status.QOSClass)},
		{Key: "Service account", Value: p.Spec.ServiceAccountName, Link: linkIf("v1/serviceaccounts", p.Namespace, p.Spec.ServiceAccountName)},
	}}
	if p.Status.Reason != "" {
		status.Fields = append(status.Fields, Field{Key: "Reason", Value: p.Status.Reason + " " + p.Status.Message, Tone: ToneErr})
	}
	stat := map[string]*v1.ContainerStatus{}
	for i := range p.Status.InitContainerStatuses {
		stat["i:"+p.Status.InitContainerStatuses[i].Name] = &p.Status.InitContainerStatuses[i]
	}
	for i := range p.Status.ContainerStatuses {
		stat["c:"+p.Status.ContainerStatuses[i].Name] = &p.Status.ContainerStatuses[i]
	}
	cont := Section{Title: fmt.Sprintf("Containers (%d)", len(p.Spec.Containers))}
	for _, co := range p.Spec.InitContainers {
		cont.Cards = append(cont.Cards, containerCard(p.Namespace, co, stat["i:"+co.Name], true))
	}
	for _, co := range p.Spec.Containers {
		cont.Cards = append(cont.Cards, containerCard(p.Namespace, co, stat["c:"+co.Name], false))
	}
	out := []Section{status, cont}
	if v := volumesSection(p.Namespace, p.Spec.Volumes); v != nil {
		out = append(out, *v)
	}
	return out
}

func containerTemplate(ns string, t *v1.PodTemplateSpec) Section {
	s := Section{Title: "Pod template"}
	for _, co := range t.Spec.Containers {
		s.Cards = append(s.Cards, containerCard(ns, co, nil, false))
	}
	return s
}

// podsFor lists pods matching a selector as a linked table.
func (c *Cluster) podsFor(ns string, sel labels.Selector) *Section {
	pods := listTyped[v1.Pod](c, client.PodGVR, ns)
	t := &DTable{Columns: []string{"POD", "STATUS", "RESTARTS", "NODE", "AGE"}}
	for _, p := range pods {
		if !sel.Matches(labels.Set(p.Labels)) {
			continue
		}
		var restarts int32
		for _, s := range p.Status.ContainerStatuses {
			restarts += s.RestartCount
		}
		phase := podPhase(&p)
		tone := map[string]Tone{"Running": ToneOK, "Pending": ToneWarn, "Failing": ToneErr, "Failed": ToneErr, "Succeeded": ToneDim}[phase]
		t.Rows = append(t.Rows, []Field{
			{Value: p.Name, Link: &Link{GVR: "v1/pods", Path: nsPath(p.Namespace, p.Name)}},
			{Value: phase, Tone: tone},
			{Value: fmt.Sprint(restarts)},
			{Value: p.Spec.NodeName, Tone: ToneDim},
			{Value: age(p.CreationTimestamp), Tone: ToneDim},
		})
	}
	if len(t.Rows) == 0 {
		return &Section{Title: "Pods", Text: "No pods match the selector."}
	}
	return &Section{Title: fmt.Sprintf("Pods (%d)", len(t.Rows)), Table: t}
}

func (c *Cluster) workloadSections(ns string, ls *metav1.LabelSelector, tpl *v1.PodTemplateSpec, fields []Field) []Section {
	out := []Section{{Title: "Rollout", Fields: fields}}
	if ls != nil {
		if sel, err := metav1.LabelSelectorAsSelector(ls); err == nil {
			out[0].Fields = append(out[0].Fields, Field{Key: "Selector", Value: sel.String(), Mono: true})
			out = append(out, *c.podsFor(ns, sel))
		}
	}
	return append(out, containerTemplate(ns, tpl))
}

func (c *Cluster) serviceSections(s *v1.Service) []Section {
	f := []Field{
		{Key: "Type", Value: string(s.Spec.Type)},
		{Key: "Cluster IP", Value: s.Spec.ClusterIP, Mono: true},
	}
	for _, ing := range s.Status.LoadBalancer.Ingress {
		f = append(f, Field{Key: "Load balancer", Value: ing.IP + ing.Hostname, Mono: true})
	}
	pt := &DTable{Columns: []string{"NAME", "PORT", "TARGET", "NODEPORT"}}
	for _, p := range s.Spec.Ports {
		np := ""
		if p.NodePort != 0 {
			np = fmt.Sprint(p.NodePort)
		}
		pt.Rows = append(pt.Rows, []Field{{Value: p.Name}, {Value: fmt.Sprintf("%d/%s", p.Port, p.Protocol), Mono: true}, {Value: p.TargetPort.String(), Mono: true}, {Value: np, Mono: true}})
	}
	out := []Section{{Title: "Service", Fields: f, Table: pt}}
	if len(s.Spec.Selector) > 0 {
		sel := labels.SelectorFromSet(s.Spec.Selector)
		out[0].Fields = append(out[0].Fields, Field{Key: "Selector", Value: sel.String(), Mono: true})
		ps := c.podsFor(s.Namespace, sel)
		ps.Title = strings.Replace(ps.Title, "Pods", "Endpoints (pods)", 1)
		if ps.Table == nil {
			ps.Text = "No pods match the selector: the service has no endpoints."
		}
		out = append(out, *ps)
	}
	return out
}

func ingressSection(in *netv1.Ingress) Section {
	t := &DTable{Columns: []string{"HOST", "PATH", "BACKEND"}}
	for _, r := range in.Spec.Rules {
		if r.HTTP == nil {
			continue
		}
		for _, p := range r.HTTP.Paths {
			b := Field{Value: "-"}
			if p.Backend.Service != nil {
				port := p.Backend.Service.Port.Name
				if port == "" {
					port = fmt.Sprint(p.Backend.Service.Port.Number)
				}
				b = Field{Value: p.Backend.Service.Name + ":" + port, Link: &Link{GVR: "v1/services", Path: nsPath(in.Namespace, p.Backend.Service.Name)}}
			}
			host := r.Host
			if host == "" {
				host = "*"
			}
			t.Rows = append(t.Rows, []Field{{Value: host, Mono: true}, {Value: p.Path, Mono: true}, b})
		}
	}
	f := []Field{}
	if in.Spec.IngressClassName != nil {
		f = append(f, Field{Key: "Class", Value: *in.Spec.IngressClassName})
	}
	for _, tl := range in.Spec.TLS {
		f = append(f, Field{Key: "TLS", Value: strings.Join(tl.Hosts, ", ") + " → secret " + tl.SecretName, Link: linkIf("v1/secrets", in.Namespace, tl.SecretName)})
	}
	return Section{Title: "Routing", Fields: f, Table: t}
}

func (c *Cluster) nodeSections(n *v1.Node) []Section {
	info := n.Status.NodeInfo
	f := []Field{
		{Key: "Kubelet", Value: info.KubeletVersion},
		{Key: "OS / arch", Value: info.OSImage + " · " + info.Architecture},
		{Key: "Runtime", Value: info.ContainerRuntimeVersion},
		{Key: "Kernel", Value: info.KernelVersion},
	}
	if n.Spec.Unschedulable {
		f = append(f, Field{Key: "Scheduling", Value: "disabled (cordoned)", Tone: ToneWarn})
	}
	for _, a := range n.Status.Addresses {
		f = append(f, Field{Key: string(a.Type), Value: a.Address, Mono: true})
	}
	res := &DTable{Columns: []string{"RESOURCE", "CAPACITY", "ALLOCATABLE"}}
	for _, r := range []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory, v1.ResourcePods, v1.ResourceEphemeralStorage} {
		cq, aq := n.Status.Capacity[r], n.Status.Allocatable[r]
		res.Rows = append(res.Rows, []Field{{Value: string(r)}, {Value: HumanQuantity(r, cq), Mono: true}, {Value: HumanQuantity(r, aq), Mono: true}})
	}
	out := []Section{{Title: "Node", Fields: f}, {Title: "Resources", Table: res}}
	if len(n.Spec.Taints) > 0 {
		tt := &DTable{Columns: []string{"TAINT", "EFFECT"}}
		for _, t := range n.Spec.Taints {
			k := t.Key
			if t.Value != "" {
				k += "=" + t.Value
			}
			tt.Rows = append(tt.Rows, []Field{{Value: k, Mono: true}, {Value: string(t.Effect), Tone: ToneWarn}})
		}
		out = append(out, Section{Title: "Taints", Table: tt})
	}
	// pods on the node
	pods := listTyped[v1.Pod](c, client.PodGVR, client.BlankNamespace)
	pt := &DTable{Columns: []string{"POD", "STATUS"}}
	for _, p := range pods {
		if p.Spec.NodeName != n.Name {
			continue
		}
		ph := podPhase(&p)
		pt.Rows = append(pt.Rows, []Field{{Value: p.Namespace + "/" + p.Name, Link: &Link{GVR: "v1/pods", Path: nsPath(p.Namespace, p.Name)}}, {Value: ph, Tone: map[string]Tone{"Running": ToneOK, "Pending": ToneWarn, "Failing": ToneErr, "Failed": ToneErr, "Succeeded": ToneDim}[ph]}})
	}
	out = append(out, Section{Title: fmt.Sprintf("Pods on node (%d)", len(pt.Rows)), Table: pt})
	return out
}

// usedBy lists pods that reference a configmap/secret/pvc.
func (c *Cluster) usedBy(ns, kind, name string) Section {
	pods := listTyped[v1.Pod](c, client.PodGVR, ns)
	t := &DTable{Columns: []string{"POD", "HOW"}}
	for _, p := range pods {
		how := podRefs(&p, kind, name)
		if how != "" {
			t.Rows = append(t.Rows, []Field{{Value: p.Name, Link: &Link{GVR: "v1/pods", Path: nsPath(p.Namespace, p.Name)}}, {Value: how, Tone: ToneDim}})
		}
	}
	if len(t.Rows) == 0 {
		return Section{Title: "Used by", Text: "Not referenced by any pod in this namespace.", Badge: Field{Value: "unused", Tone: ToneWarn}}
	}
	return Section{Title: fmt.Sprintf("Used by (%d pods)", len(t.Rows)), Table: t}
}

// podRefs describes how a pod references kind/name ("" = not at all).
func podRefs(p *v1.Pod, kind, name string) string {
	var how []string
	for _, v := range p.Spec.Volumes {
		switch {
		case kind == "configmap" && v.ConfigMap != nil && v.ConfigMap.Name == name,
			kind == "secret" && v.Secret != nil && v.Secret.SecretName == name,
			kind == "pvc" && v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == name:
			how = append(how, "volume "+v.Name)
		case v.Projected != nil:
			for _, s := range v.Projected.Sources {
				if (kind == "configmap" && s.ConfigMap != nil && s.ConfigMap.Name == name) || (kind == "secret" && s.Secret != nil && s.Secret.Name == name) {
					how = append(how, "projected volume "+v.Name)
				}
			}
		}
	}
	for _, s := range p.Spec.ImagePullSecrets {
		if kind == "secret" && s.Name == name {
			how = append(how, "imagePullSecret")
		}
	}
	all := append(append([]v1.Container{}, p.Spec.InitContainers...), p.Spec.Containers...)
	for _, co := range all {
		for _, e := range co.Env {
			if e.ValueFrom == nil {
				continue
			}
			if (kind == "configmap" && e.ValueFrom.ConfigMapKeyRef != nil && e.ValueFrom.ConfigMapKeyRef.Name == name) ||
				(kind == "secret" && e.ValueFrom.SecretKeyRef != nil && e.ValueFrom.SecretKeyRef.Name == name) {
				how = append(how, "env "+co.Name+"/"+e.Name)
			}
		}
		for _, ef := range co.EnvFrom {
			if (kind == "configmap" && ef.ConfigMapRef != nil && ef.ConfigMapRef.Name == name) ||
				(kind == "secret" && ef.SecretRef != nil && ef.SecretRef.Name == name) {
				how = append(how, "envFrom "+co.Name)
			}
		}
	}
	return strings.Join(how, ", ")
}
