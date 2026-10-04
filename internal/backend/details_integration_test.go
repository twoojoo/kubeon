//go:build integration && dev

package backend

import (
	"strings"
	"testing"
)

func sectionTitles(d *Details) string {
	var t []string
	for _, s := range d.Sections {
		t = append(t, s.Title)
	}
	return strings.Join(t, " | ")
}

func findSection(d *Details, prefix string) *Section {
	for i := range d.Sections {
		if strings.HasPrefix(d.Sections[i].Title, prefix) {
			return &d.Sections[i]
		}
	}
	return nil
}

func TestDetails(t *testing.T) {
	c := devCluster(t)
	c.Watch("v1/pods", "", func(*Snapshot) {}).Close()
	s, err := c.SnapshotOnce(t.Context(), "v1/pods", "demo", "^demo/web-")
	if err != nil || len(s.Rows) == 0 {
		t.Fatalf("pods %v", err)
	}
	pod := s.Rows[0].ID

	d, err := c.Details("v1/pods", pod)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("pod: %s", sectionTitles(d))
	cs := findSection(d, "Containers")
	if cs == nil || len(cs.Cards) != 2 || cs.Cards[0].Badge.Tone != ToneOK {
		t.Fatalf("containers %+v", cs)
	}
	if findSection(d, "Conditions") == nil || findSection(d, "Volumes") == nil {
		t.Fatal("missing conditions/volumes")
	}
	owner := d.Sections[0].Fields
	found := false
	for _, f := range owner {
		if f.Key == "Controlled by" && f.Link != nil && f.Link.GVR == "apps/v1/replicasets" {
			found = true
		}
	}
	if !found {
		t.Fatalf("owner link missing: %+v", owner)
	}

	dep, err := c.Details("apps/v1/deployments", "demo/web")
	if err != nil {
		t.Fatal(err)
	}
	if ps := findSection(dep, "Pods ("); ps == nil || len(ps.Table.Rows) != 3 {
		t.Fatalf("deployment pods: %s", sectionTitles(dep))
	}
	svc, err := c.Details("v1/services", "demo/web")
	if err != nil || findSection(svc, "Endpoints (pods)") == nil {
		t.Fatalf("service: %v %s", err, sectionTitles(svc))
	}
	cm, err := c.Details("v1/configmaps", "demo/settings")
	if err != nil || findSection(cm, "Used by") == nil {
		t.Fatalf("configmap: %v", err)
	}
	node, err := c.Details("v1/nodes", "k9desk-dev-control-plane")
	if err != nil || findSection(node, "Pods on node") == nil {
		t.Fatalf("node: %v", err)
	}
	sec, err := c.Details("v1/secrets", "demo/db-creds")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sec.Sections {
		if s.Table != nil {
			for _, r := range s.Table.Rows {
				for _, f := range r {
					if strings.Contains(f.Value, "s3cr3t") {
						t.Fatal("secret value leaked into details")
					}
				}
			}
		}
	}
}
