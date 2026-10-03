//go:build integration && dev

package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giovanni/k9desk/internal/safety"
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
