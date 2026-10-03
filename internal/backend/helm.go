package backend

import (
	"context"
	"fmt"
	"strings"

	"github.com/giovanni/k9desk/third_party/k9s/client"
	"github.com/giovanni/k9desk/third_party/k9s/dao"
	"github.com/giovanni/k9desk/third_party/k9s/render/helm"
)

// Helm pseudo-resources served by k9s' helm DAOs.
const (
	HelmGVR        = "helm"
	HelmHistoryGVR = "helm-history"
)

// HelmValues returns user-supplied (or all) values of a release; path may
// be "ns/name" or a history row "ns/name:rev".
func (c *Cluster) HelmValues(path string, all bool) (string, error) {
	var (
		b   []byte
		err error
	)
	if strings.Contains(path, ":") {
		h, ok := c.Accessor(HelmHistoryGVR).(*dao.HelmHistory)
		if !ok {
			return "", fmt.Errorf("helm history unavailable")
		}
		b, err = h.GetValues(path, all)
	} else {
		h, ok := c.Accessor(HelmGVR).(*dao.HelmChart)
		if !ok {
			return "", fmt.Errorf("helm unavailable")
		}
		b, err = h.GetValues(path, all)
	}
	return string(b), err
}

// HelmRollback rolls a release back to revision rev.
func (c *Cluster) HelmRollback(release string, rev string) error {
	h, ok := c.Accessor(HelmHistoryGVR).(*dao.HelmHistory)
	if !ok {
		return fmt.Errorf("helm history unavailable")
	}
	return h.Rollback(context.Background(), release, rev)
}

// HelmUninstall uninstalls a release.
func (c *Cluster) HelmUninstall(release string) error {
	h, ok := c.Accessor(HelmGVR).(*dao.HelmChart)
	if !ok {
		return fmt.Errorf("helm unavailable")
	}
	return h.Uninstall(release, false)
}

// SplitHistoryID splits "ns/name:rev".
func SplitHistoryID(id string) (release, rev string) {
	i := strings.LastIndexByte(id, ':')
	if i < 0 {
		return id, ""
	}
	return id[:i], id[i+1:]
}

// HelmDescribe summarizes a release (or history revision "ns/name:rev").
func (c *Cluster) HelmDescribe(path string) (string, error) {
	var (
		o   any
		err error
	)
	if strings.Contains(path, ":") {
		o, err = c.Accessor(HelmHistoryGVR).Get(context.Background(), path)
	} else {
		o, err = c.Accessor(HelmGVR).Get(context.Background(), path)
	}
	if err != nil {
		return "", err
	}
	rr, ok := o.(helm.ReleaseRes)
	if !ok || rr.Release == nil {
		return "", fmt.Errorf("unexpected %T", o)
	}
	r := rr.Release
	var b strings.Builder
	fmt.Fprintf(&b, "Name:         %s\nNamespace:    %s\nRevision:     %d\n", r.Name, r.Namespace, r.Version)
	if r.Info != nil {
		fmt.Fprintf(&b, "Status:       %s\nDeployed:     %s\nDescription:  %s\n",
			r.Info.Status, r.Info.LastDeployed.Format("2006-01-02 15:04:05"), r.Info.Description)
	}
	if r.Chart != nil && r.Chart.Metadata != nil {
		fmt.Fprintf(&b, "Chart:        %s-%s\nApp version:  %s\n", r.Chart.Metadata.Name, r.Chart.Metadata.Version, r.Chart.Metadata.AppVersion)
	}
	if r.Info != nil && r.Info.Notes != "" {
		fmt.Fprintf(&b, "\nNOTES:\n%s\n", r.Info.Notes)
	}
	fmt.Fprintf(&b, "\nMANIFEST:\n%s", r.Manifest)
	return b.String(), nil
}

var _ = client.HmGVR
