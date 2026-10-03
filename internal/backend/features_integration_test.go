//go:build integration && dev

package backend

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestNodeCordon(t *testing.T) {
	c := devCluster(t)
	const node = "k9desk-dev-control-plane"
	if err := c.Cordon(node, true); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Cordon(node, false) }()
	ch := make(chan *Snapshot, 16)
	tw := c.Watch("v1/nodes", "", func(s *Snapshot) { ch <- s })
	defer tw.Close()
	waitSnap(t, ch, func(s *Snapshot) bool {
		return len(s.Rows) == 1 && strings.Contains(strings.Join(s.Rows[0].Cells, " "), "SchedulingDisabled")
	})
	if err := c.Cordon(node, false); err != nil {
		t.Fatal(err)
	}
	waitSnap(t, ch, func(s *Snapshot) bool {
		return len(s.Rows) == 1 && !strings.Contains(strings.Join(s.Rows[0].Cells, " "), "SchedulingDisabled")
	})
}

func TestSecretData(t *testing.T) {
	c := devCluster(t)
	c.Watch("v1/secrets", "demo", func(*Snapshot) {}).Close()
	var ee []SecretEntry
	var err error
	for i := 0; i < 50; i++ {
		if ee, err = c.SecretData("demo/db-creds"); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, e := range ee {
		m[e.Key] = string(e.Value)
	}
	if m["password"] != "s3cr3t!" || m["username"] != "admin" {
		t.Fatalf("got %v", m)
	}
}

func TestMultiPodJSONLogs(t *testing.T) {
	c := devCluster(t)
	c.Watch("apps/v1/deployments", "demo", func(*Snapshot) {}).Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pods := map[string]bool{}
	got := make(chan []LogLine, 64)
	if err := c.TailLogs(ctx, "apps/v1/deployments", "demo/jsonlog", LogOpts{AllContainers: true, TailLines: 20}, func(l []LogLine) { got <- l }); err != nil {
		t.Fatal(err)
	}
	for len(pods) < 2 {
		select {
		case batch := <-got:
			for _, l := range batch {
				if l.Pod != "" {
					pods[l.Pod] = true
				}
				if strings.Contains(l.Text, "[[]") || (strings.HasPrefix(l.Text, "{") && !strings.HasSuffix(l.Text, "}")) {
					t.Fatalf("mangled json line %q", l.Text)
				}
			}
		case <-ctx.Done():
			t.Fatalf("pods seen: %v", pods)
		}
	}
}

func installDemoChart(t *testing.T, c *Cluster) {
	t.Helper()
	ns, kc := "demo", c.Kubeconfig
	flags := &genericclioptions.ConfigFlags{KubeConfig: &kc, Namespace: &ns}
	cfg := new(action.Configuration)
	if err := cfg.Init(flags, ns, "secret", func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	ch, err := loader.Load(filepath.Join("..", "..", "testdata", "charts", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = action.NewUninstall(cfg).Run("demo-rel")
	in := action.NewInstall(cfg)
	in.ReleaseName, in.Namespace = "demo-rel", ns
	if _, err := in.Run(ch, map[string]any{"greeting": "ciao"}); err != nil {
		t.Fatal(err)
	}
	up := action.NewUpgrade(cfg)
	up.Namespace = ns
	if _, err := up.Run("demo-rel", ch, map[string]any{"greeting": "hola"}); err != nil {
		t.Fatal(err)
	}
}

func TestHelm(t *testing.T) {
	c := devCluster(t)
	installDemoChart(t, c)
	ch := make(chan *Snapshot, 16)
	tw := c.Watch(HelmGVR, "demo", func(s *Snapshot) { ch <- s })
	defer tw.Close()
	s := waitSnap(t, ch, func(s *Snapshot) bool { return hasRow(s, "demo/demo-rel") })
	t.Logf("helm cols %v", s.Columns)

	hch := make(chan *Snapshot, 16)
	hw := c.WatchPath(HelmHistoryGVR, "demo", "demo/demo-rel", func(s *Snapshot) { hch <- s })
	defer hw.Close()
	waitSnap(t, hch, func(s *Snapshot) bool { return len(s.Rows) == 2 })

	v, err := c.HelmValues("demo/demo-rel", false)
	if err != nil || !strings.Contains(v, "hola") {
		t.Fatalf("values %q %v", v, err)
	}
	if d, err := c.Describe(HelmGVR, "demo/demo-rel"); err != nil || d == "" {
		t.Fatalf("describe %v", err)
	}
	if err := c.HelmRollback("demo/demo-rel", "1"); err != nil {
		t.Fatal(err)
	}
	waitSnap(t, hch, func(s *Snapshot) bool { return len(s.Rows) == 3 })
	if v, _ := c.HelmValues("demo/demo-rel", false); !strings.Contains(v, "ciao") {
		t.Fatalf("rollback values %q", v)
	}
	if err := c.HelmUninstall("demo/demo-rel"); err != nil {
		t.Fatal(err)
	}
	waitSnap(t, ch, func(s *Snapshot) bool { return !hasRow(s, "demo/demo-rel") })
}

func TestXRay(t *testing.T) {
	c := devCluster(t)
	if !CanXRay("apps/v1/deployments") {
		t.Fatal("deployments should support xray")
	}
	got := make(chan *XNode, 4)
	w := c.XRay("apps/v1/deployments", "demo", func(n *XNode, err error) {
		if err != nil {
			t.Logf("xray err %v", err)
			return
		}
		got <- n
	})
	defer w.Close()
	deadline := time.After(30 * time.Second)
	for {
		var root *XNode
		select {
		case root = <-got:
		case <-deadline:
			t.Fatal("no complete xray tree")
		}
		if root.Count() < 5 {
			continue
		}
		var dump func(n *XNode, d int)
		dump = func(n *XNode, d int) {
			t.Logf("%s%s %s [%s] %s", strings.Repeat("  ", d), n.Kind, n.Name, n.Status, n.Info)
			for _, k := range n.Children {
				dump(k, d+1)
			}
		}
		dump(root, 0)
		return
	}
}

func TestHealthEvents(t *testing.T) {
	c := devCluster(t)
	c.Watch("v1/pods", "", func(*Snapshot) {}).Close()
	st := healthState{pods: map[string]string{}, nodes: map[string]bool{}, deps: map[string]bool{}}
	if ev := c.scanHealth(&st, false); len(ev) != 0 {
		t.Fatal("seed must be silent")
	}
	found := false
	for k, v := range st.pods {
		if strings.HasPrefix(k, "demo/crasher-") && v == "CrashLoopBackOff" {
			found = true
		}
	}
	if !found {
		t.Logf("pods state %v", st.pods)
		t.Fatal("crasher not detected as failing")
	}
	// simulate a healthy previous state → transition must be reported
	for k := range st.pods {
		if strings.HasPrefix(k, "demo/crasher-") {
			st.pods[k] = ""
		}
	}
	ev := c.scanHealth(&st, true)
	if len(ev) == 0 || ev[0].Severity != SevErr {
		t.Fatalf("events %+v", ev)
	}
}
