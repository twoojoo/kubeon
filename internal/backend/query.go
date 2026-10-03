package backend

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"

	"github.com/giovanni/k9desk/third_party/k9s/client"
)

// SnapshotOnce returns the first loaded table of gvr in ns (one-shot
// watch). filter is a case-insensitive regex matched against row cells.
func (c *Cluster) SnapshotOnce(ctx context.Context, gvr, ns, filter string) (*Snapshot, error) {
	ch := make(chan *Snapshot, 8)
	tw := c.Watch(gvr, ns, func(s *Snapshot) {
		select {
		case ch <- s:
		default:
		}
	})
	defer tw.Close()
	var rx *regexp.Regexp
	if filter != "" {
		var err error
		if rx, err = regexp.Compile("(?i)" + filter); err != nil {
			rx = regexp.MustCompile("(?i)" + regexp.QuoteMeta(filter))
		}
	}
	deadline := time.After(20 * time.Second)
	for {
		select {
		case s := <-ch:
			if s.Loading {
				continue
			}
			if s.Err != nil && len(s.Rows) == 0 {
				return nil, s.Err
			}
			if rx != nil {
				rows := s.Rows[:0:0]
				for _, r := range s.Rows {
					if rx.MatchString(r.ID + " " + strings.Join(r.Cells, " ")) {
						rows = append(rows, r)
					}
				}
				cp := *s
				cp.Rows = rows
				s = &cp
			}
			return s, nil
		case <-deadline:
			return nil, fmt.Errorf("timeout listing %s", gvr)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// EventInfoFull is an event with its type.
type EventInfoFull struct {
	EventInfo
	Type string
}

// Events lists events in ns ("" = all), optionally for objects whose name
// contains obj, newest first.
func (c *Cluster) Events(ns, obj string, warningsOnly bool, limit int) []EventInfoFull {
	pns := ns
	if pns == "" {
		pns = client.BlankNamespace
	}
	// make sure the informer exists and is synced
	_, _ = c.factory.CanForResource(pns, client.NewGVR("v1/events"), client.ListAccess)
	var out []EventInfoFull
	for _, e := range listTyped[v1.Event](c, client.NewGVR("v1/events"), pns) {
		if warningsOnly && e.Type != v1.EventTypeWarning {
			continue
		}
		if obj != "" && !strings.Contains(e.InvolvedObject.Name, obj) {
			continue
		}
		last := e.LastTimestamp.Time
		if last.IsZero() {
			last = e.EventTime.Time
		}
		if last.IsZero() {
			last = e.CreationTimestamp.Time
		}
		out = append(out, EventInfoFull{Type: e.Type, EventInfo: EventInfo{
			Namespace: e.Namespace,
			Object:    e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
			Reason:    e.Reason,
			Message:   e.Message,
			Count:     e.Count,
			Last:      last,
		}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// FetchLogs returns the last lines of a loggable resource without following.
func (c *Cluster) FetchLogs(ctx context.Context, gvr, path string, o LogOpts, settle time.Duration) ([]LogLine, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if settle == 0 {
		settle = 1500 * time.Millisecond
	}
	ch := make(chan []LogLine, 64)
	if err := c.TailLogs(ctx, gvr, path, o, func(l []LogLine) {
		select {
		case ch <- l:
		case <-ctx.Done():
		}
	}); err != nil {
		return nil, err
	}
	var out []LogLine
	quiet := time.NewTimer(settle)
	hard := time.After(10 * time.Second)
	for {
		select {
		case l := <-ch:
			out = append(out, l...)
			if !quiet.Stop() {
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(400 * time.Millisecond)
		case <-quiet.C:
			return out, nil
		case <-hard:
			return out, nil
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
}

// XRayOnce builds a single xray tree.
func (c *Cluster) XRayOnce(ctx context.Context, gvr, ns string) (*XNode, error) {
	type res struct {
		n   *XNode
		err error
	}
	ch := make(chan res, 4)
	w := c.XRay(gvr, ns, func(n *XNode, err error) {
		select {
		case ch <- res{n, err}:
		default:
		}
	})
	defer w.Close()
	var best *XNode
	timeout := time.After(15 * time.Second)
	for {
		select {
		case r := <-ch:
			if r.err != nil {
				return nil, r.err
			}
			best = r.n
			if best != nil && best.Count() > 1 {
				return best, nil
			}
		case <-timeout:
			if best != nil {
				return best, nil
			}
			return nil, fmt.Errorf("xray timeout")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
