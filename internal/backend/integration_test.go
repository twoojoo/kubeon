//go:build integration && dev

package backend

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/giovanni/kubeon/internal/safety"
	"github.com/giovanni/kubeon/third_party/k9s/client"
)

// Runs only against the disposable kind cluster (.dev/kubeconfig).
func devCluster(t *testing.T) *Cluster {
	t.Helper()
	kc, err := safety.ResolveKubeconfig(filepath.Join("..", "..", ".dev", "kubeconfig"))
	if err != nil {
		t.Fatal(err)
	}
	safety.IsolateCaches(kc)
	if _, err := os.Stat(kc); err != nil {
		t.Skip("dev cluster not running: scripts/dev-cluster.sh up")
	}
	names, cur, err := Contexts(kc)
	if err != nil {
		t.Fatal(err)
	}
	if len(safety.FilterContexts(names)) != len(names) || safety.CheckContext(cur) != nil {
		t.Fatalf("refusing kubeconfig with foreign contexts: %v", names)
	}
	c, err := Connect(kc, cur)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func waitSnap(t *testing.T, ch <-chan *Snapshot, ok func(*Snapshot) bool) *Snapshot {
	t.Helper()
	to := time.After(30 * time.Second)
	for {
		select {
		case s := <-ch:
			if s.Err != nil {
				t.Logf("snapshot err: %v", s.Err)
			}
			if ok(s) {
				return s
			}
		case <-to:
			t.Fatal("timeout waiting snapshot")
		}
	}
}

func TestWatchPods(t *testing.T) {
	c := devCluster(t)
	ch := make(chan *Snapshot, 16)
	tw := c.Watch("v1/pods", "demo", func(s *Snapshot) { ch <- s })
	defer tw.Close()
	s := waitSnap(t, ch, func(s *Snapshot) bool { return len(s.Rows) >= 4 })
	t.Logf("cols=%v", s.Columns)
	for _, r := range s.Rows {
		t.Logf("%-40s state=%d %v", r.ID, r.State, r.Cells)
	}
	tw.SetFilter("crasher")
	s = waitSnap(t, ch, func(s *Snapshot) bool { return len(s.Rows) == 1 })
	if s.Total < 4 {
		t.Fatalf("total %d", s.Total)
	}
}

func TestWatchAllNamespacesDeployments(t *testing.T) {
	c := devCluster(t)
	ch := make(chan *Snapshot, 16)
	tw := c.Watch("apps/v1/deployments", "", func(s *Snapshot) { ch <- s })
	defer tw.Close()
	s := waitSnap(t, ch, func(s *Snapshot) bool { return len(s.Rows) >= 3 })
	if s.Columns[0].Name != "NAMESPACE" {
		t.Fatalf("expected NAMESPACE col, got %v", s.Columns)
	}
}

func TestCRDAndYAMLAndLogs(t *testing.T) {
	c := devCluster(t)
	found := false
	for _, r := range c.Resources() {
		if r.CRD && r.Kind == "Widget" {
			found = true
		}
	}
	if !found {
		t.Fatal("widget CRD not discovered")
	}
	ch := make(chan *Snapshot, 16)
	tw := c.Watch("v1/pods", "demo", func(s *Snapshot) { ch <- s })
	defer tw.Close()
	var web string
	waitSnap(t, ch, func(s *Snapshot) bool {
		for _, r := range s.Rows {
			if len(r.ID) > 9 && r.ID[:9] == "demo/web-" && r.Cells[3] == "Running" {
				web = r.ID
				return true
			}
		}
		return false
	})
	y, err := c.YAML("v1/pods", web)
	if err != nil || len(y) < 100 {
		t.Fatalf("yaml: %v %d", err, len(y))
	}
	d, err := c.Describe("v1/pods", web)
	if err != nil || len(d) < 100 {
		t.Fatalf("describe: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	lines := make(chan []LogLine, 8)
	if err := c.TailLogs(ctx, "v1/pods", web, LogOpts{AllContainers: true, TailLines: 5}, func(l []LogLine) { lines <- l }); err != nil {
		t.Fatal(err)
	}
	select {
	case l := <-lines:
		t.Logf("logs: %+v", l[0])
	case <-ctx.Done():
		t.Fatal("no logs")
	}
}

func TestPortForward(t *testing.T) {
	c := devCluster(t)
	ch := make(chan *Snapshot, 16)
	tw := c.Watch("apps/v1/deployments", "demo", func(s *Snapshot) { ch <- s })
	defer tw.Close()
	waitSnap(t, ch, func(s *Snapshot) bool { return len(s.Rows) >= 2 })
	ports, err := c.ContainerPorts("apps/v1/deployments", "demo/web")
	if err != nil || len(ports) == 0 {
		t.Fatalf("ports %v %v", ports, err)
	}
	pf, err := c.PortForward("apps/v1/deployments", "demo/web", 0, ports[0])
	if err != nil {
		t.Fatal(err)
	}
	defer c.StopPortForward(pf.ID)
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", pf.Local))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "kubeon-ok") {
		t.Fatalf("body %q", b)
	}
	if len(c.PortForwards()) != 1 {
		t.Fatal("forward not tracked")
	}
}

func TestOverview(t *testing.T) {
	c := devCluster(t)
	c.Watch("v1/pods", "", func(*Snapshot) {}).Close()
	o := c.Overview("")
	t.Logf("%+v", o)
	if o.Nodes != 1 || o.Pods < 5 || o.CPUCapacity == 0 || o.Deployments < 2 {
		t.Fatalf("bad overview %+v", o)
	}
	if o.DeploymentsBad < 1 {
		t.Fatal("crasher should be unavailable")
	}
}

func TestMutations(t *testing.T) {
	c := devCluster(t)
	const cm = `apiVersion: v1
kind: ConfigMap
metadata: {name: kubeon-mut, namespace: demo}
data: {v: "1"}
`
	_ = c.Delete("v1/configmaps", "demo/kubeon-mut", true)
	dial, _ := c.conn.DynDial()
	var u unstructuredObj
	if err := yamlToObj(cm, &u); err != nil {
		t.Fatal(err)
	}
	if _, err := dial.Resource(client.CmGVR.GVR()).Namespace("demo").Create(context.Background(), u.obj(), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	ch := make(chan *Snapshot, 16)
	tw := c.Watch("v1/configmaps", "demo", func(s *Snapshot) { ch <- s })
	defer tw.Close()
	waitSnap(t, ch, func(s *Snapshot) bool { return hasRow(s, "demo/kubeon-mut") })

	y, err := c.YAML("v1/configmaps", "demo/kubeon-mut")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Apply("v1/configmaps", strings.Replace(y, `v: "1"`, `v: "2"`, 1)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		y2, _ := c.YAML("v1/configmaps", "demo/kubeon-mut")
		if strings.Contains(y2, `v: "2"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("apply not visible:\n%s", y2)
		}
		time.Sleep(200 * time.Millisecond)
	}

	if err := c.Scale("apps/v1/deployments", "demo/web", 2); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Replicas("apps/v1/deployments", "demo/web"); err != nil || n != 2 {
		t.Logf("replicas=%d err=%v (informer lag ok)", n, err)
	}
	if err := c.Scale("apps/v1/deployments", "demo/web", 3); err != nil {
		t.Fatal(err)
	}
	if err := c.Restart("apps/v1/deployments", "demo/web"); err != nil {
		t.Fatal(err)
	}

	if err := c.Delete("v1/configmaps", "demo/kubeon-mut", false); err != nil {
		t.Fatal(err)
	}
	waitSnap(t, ch, func(s *Snapshot) bool { return !hasRow(s, "demo/kubeon-mut") })
}

func hasRow(s *Snapshot, id string) bool {
	for _, r := range s.Rows {
		if r.ID == id {
			return true
		}
	}
	return false
}

type unstructuredObj struct{ m map[string]any }

func (u *unstructuredObj) obj() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: u.m}
}

func yamlToObj(s string, u *unstructuredObj) error { return yaml.Unmarshal([]byte(s), &u.m) }
