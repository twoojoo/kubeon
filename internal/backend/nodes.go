package backend

import (
	"bytes"
	"fmt"
	"time"

	"github.com/giovanni/k9desk/third_party/k9s/dao"
)

// DrainOpts mirrors kubectl drain flags.
type DrainOpts struct {
	Force              bool
	IgnoreDaemonSets   bool
	DeleteEmptyDirData bool
	GracePeriodSeconds int // -1 = pod default
	Timeout            time.Duration
}

func (c *Cluster) nodeDAO() (*dao.Node, error) {
	n, ok := c.Accessor("v1/nodes").(*dao.Node)
	if !ok {
		return nil, fmt.Errorf("node accessor unavailable")
	}
	return n, nil
}

// Cordon marks a node (un)schedulable.
func (c *Cluster) Cordon(node string, cordon bool) error {
	n, err := c.nodeDAO()
	if err != nil {
		return err
	}
	return n.ToggleCordon(node, cordon)
}

// Drain cordons a node and evicts its pods. Returns the drain output.
func (c *Cluster) Drain(node string, o DrainOpts) (string, error) {
	n, err := c.nodeDAO()
	if err != nil {
		return "", err
	}
	if o.Timeout == 0 {
		o.Timeout = 2 * time.Minute
	}
	var out bytes.Buffer
	err = n.Drain(node, dao.DrainOptions{
		GracePeriodSeconds:  o.GracePeriodSeconds,
		Timeout:             o.Timeout,
		IgnoreAllDaemonSets: o.IgnoreDaemonSets,
		DeleteEmptyDirData:  o.DeleteEmptyDirData,
		Force:               o.Force,
	}, &out)
	return out.String(), err
}
