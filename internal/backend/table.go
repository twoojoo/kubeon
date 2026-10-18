package backend

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/derailed/tcell/v2"
	"github.com/derailed/tview"

	"github.com/twoojoo/kubeon/third_party/k9s/client"
	"github.com/twoojoo/kubeon/third_party/k9s/dao"
	"github.com/twoojoo/kubeon/third_party/k9s/model"
	"github.com/twoojoo/kubeon/third_party/k9s/model1"
	"github.com/twoojoo/kubeon/third_party/k9s/render"
)

// RowState is the semantic status of a row, derived from k9s colorers.
type RowState uint8

const (
	StateStd RowState = iota
	StateAdd
	StateMod
	StatePending
	StateErr
	StateKill
	StateCompleted
	StateHighlight
)

// k9s colorers return tcell colors; we seed them with sentinel values so
// they can be mapped back to semantic states and themed by the UI.
var stateByColor = map[tcell.Color]RowState{}

func init() {
	seed := func(c *tcell.Color, v int32, s RowState) {
		*c = tcell.NewHexColor(v)
		stateByColor[*c] = s
	}
	seed(&model1.StdColor, 0x010101, StateStd)
	seed(&model1.AddColor, 0x010102, StateAdd)
	seed(&model1.ModColor, 0x010103, StateMod)
	seed(&model1.PendingColor, 0x010104, StatePending)
	seed(&model1.ErrColor, 0x010105, StateErr)
	seed(&model1.KillColor, 0x010106, StateKill)
	seed(&model1.CompletedColor, 0x010107, StateCompleted)
	seed(&model1.HighlightColor, 0x010108, StateHighlight)
}

// Column is a visible table column.
type Column struct {
	Name       string
	AlignRight bool
}

// Row is a rendered table row.
type Row struct {
	ID    string // k9s fqn: ns/name
	Cells []string
	State RowState
}

// Snapshot is an immutable table view handed to the UI.
type Snapshot struct {
	GVR       string
	Namespace string
	Columns   []Column
	Rows      []Row
	Total     int // rows before filtering
	SortCol   string
	SortAsc   bool
	Err       error
	Loading   bool
	Seq       uint64
}

// TableWatch keeps a k9s model.Table live and publishes UI snapshots.
type TableWatch struct {
	cluster *Cluster
	gvr     *client.GVR
	ns      string
	path    string
	mt      *model.Table
	cancel  context.CancelFunc
	onSnap  func(*Snapshot)
	colorer model1.ColorerFunc

	mx      sync.Mutex
	raw     *model1.TableData
	filter  string
	sortCol string
	sortAsc bool
	wide    bool
	lastErr error
	seq     atomic.Uint64
	pending atomic.Bool
	kick    chan struct{}
	failed  chan struct{} // the k9s updater gave up
	started time.Time
	closed  atomic.Bool
}

// Watch starts watching gvr in ns ("" = all namespaces). onSnap is called
// from a background goroutine, at most every 200ms.
func (c *Cluster) Watch(gvr, ns string, onSnap func(*Snapshot)) *TableWatch {
	return c.WatchPath(gvr, ns, "", onSnap)
}

// WatchPath watches a sub-listing scoped to an owner path (e.g. helm
// history of "ns/release").
func (c *Cluster) WatchPath(gvr, ns, path string, onSnap func(*Snapshot)) *TableWatch {
	g := client.NewGVR(gvr)
	if !IsNamespaced(gvr) {
		ns = client.ClusterScope
	} else if ns == "" {
		ns = client.BlankNamespace
	}
	meta, ok := model.Registry[g]
	colorer := model1.DefaultColorer
	if ok && meta.Renderer != nil {
		colorer = meta.Renderer.ColorerFunc()
	} else if !ok {
		colorer = new(render.Table).ColorerFunc()
	}

	tw := TableWatch{
		cluster: c,
		gvr:     g,
		ns:      ns,
		path:    path,
		mt:      model.NewTable(g),
		onSnap:  onSnap,
		colorer: colorer,
		sortAsc: true,
		kick:    make(chan struct{}, 1),
		failed:  make(chan struct{}, 1),
		started: time.Now(),
	}
	tw.mt.SetNamespace(ns)
	tw.mt.SetLabelSelector(labels.Everything())
	tw.mt.SetRefreshRate(time.Second)
	tw.mt.AddListener(&tw)

	ctx, cancel := context.WithCancel(context.Background())
	tw.cancel = cancel
	go tw.publisher(ctx)
	tw.publish(&Snapshot{GVR: gvr, Namespace: ns, Loading: true})
	go tw.supervise(ctx, c.baseContext(ctx, g, ns, path))

	return &tw
}

// GVR returns the watched resource.
func (t *TableWatch) GVR() string { return t.gvr.String() }

// Namespace returns the watched namespace.
func (t *TableWatch) Namespace() string { return t.ns }

// Close stops the watch.
func (t *TableWatch) Close() {
	if t.closed.Swap(true) {
		return
	}
	t.mt.RemoveListener(t)
	t.cancel()
}

// SetFilter sets a k9s-style filter: regex, "-f fuzzy", "!inverse", "-l label=sel".
func (t *TableWatch) SetFilter(q string) {
	t.mx.Lock()
	changed := t.filter != q
	t.filter = q
	t.mx.Unlock()
	if changed {
		if sel, ok := labelSelector(q); ok {
			t.mt.SetLabelSelector(sel)
			go t.Refresh()
		} else {
			t.mt.SetLabelSelector(labels.Everything())
		}
		t.trigger()
	}
}

// SetSort sets sort column; toggles direction when same column.
func (t *TableWatch) SetSort(col string) {
	t.mx.Lock()
	if t.sortCol == col {
		t.sortAsc = !t.sortAsc
	} else {
		t.sortCol, t.sortAsc = col, true
	}
	t.mx.Unlock()
	t.trigger()
}

// SetWide toggles wide columns.
func (t *TableWatch) SetWide(w bool) {
	t.mx.Lock()
	t.wide = w
	t.mx.Unlock()
	t.trigger()
}

// Refresh forces a reconcile.
func (t *TableWatch) Refresh() {
	ctx, cancel := timeoutCtx()
	defer cancel()
	_ = t.mt.Refresh(t.cluster.baseContext(ctx, t.gvr, t.ns, t.path))
}

// TableNoData implements model.TableListener.
func (t *TableWatch) TableNoData(data *model1.TableData) { t.TableDataChanged(data) }

// TableDataChanged implements model.TableListener.
func (t *TableWatch) TableDataChanged(data *model1.TableData) {
	t.mx.Lock()
	t.raw, t.lastErr = data.Clone(), nil
	t.mx.Unlock()
	t.trigger()
}

// TableLoadFailed implements model.TableListener.
func (t *TableWatch) TableLoadFailed(err error) {
	t.setErr(err)
	// k9s' updater gives up after failing for a while: let supervise restart it
	select {
	case t.failed <- struct{}{}:
	default:
	}
}

func (t *TableWatch) setErr(err error) {
	t.mx.Lock()
	t.lastErr = err
	t.mx.Unlock()
	t.trigger()
}

// supervise runs the k9s table watch and restarts it (with back-off) when
// the initial load fails or its updater gives up (API errors, sleep, VPN
// drops), for as long as the view is open.
func (t *TableWatch) supervise(ctx, wctx context.Context) {
	const maxDelay = 30 * time.Second
	delay := time.Second
	for {
		if err := t.mt.Watch(wctx); err != nil {
			t.setErr(err)
		} else {
			delay = time.Second
			select {
			case <-ctx.Done():
				return
			case <-t.failed:
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(2*delay, maxDelay)
	}
}

func (t *TableWatch) trigger() {
	select {
	case t.kick <- struct{}{}:
	default:
	}
}

// publisher coalesces updates: at most one snapshot build per 150ms.
func (t *TableWatch) publisher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.kick:
			t.publish(t.build())
			select {
			case <-ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
		}
	}
}

func (t *TableWatch) publish(s *Snapshot) {
	if t.closed.Load() || s == nil {
		return
	}
	s.Seq = t.seq.Add(1)
	t.onSnap(s)
}

func (t *TableWatch) build() *Snapshot {
	t.mx.Lock()
	raw, filter, sortCol, sortAsc, wide, lastErr := t.raw, t.filter, t.sortCol, t.sortAsc, t.wide, t.lastErr
	t.mx.Unlock()

	s := Snapshot{GVR: t.gvr.String(), Namespace: t.ns, Err: lastErr, SortCol: sortCol, SortAsc: sortAsc}
	if raw == nil {
		s.Loading = lastErr == nil
		return &s
	}
	data := raw.Clone()
	s.Total = data.RowCount()
	if s.Total == 0 && lastErr == nil && time.Since(t.started) < 1500*time.Millisecond {
		// informers may still be syncing: don't flash "no resources"
		s.Loading = true
		go func() {
			time.Sleep(1600 * time.Millisecond)
			t.trigger()
		}()
		return &s
	}
	if filter != "" {
		if _, isLabel := labelSelector(filter); !isLabel {
			data = data.Filter(model1.FilterOpts{Filter: filter})
		}
	}
	h := data.Header()
	if sortCol == "" {
		sortCol = defaultSortCol(h)
		s.SortCol = sortCol
	}
	data.Sort(model1.SortColumn{Name: sortCol, ASC: sortAsc})

	ns := data.GetNamespace()
	keep := make([]int, 0, len(h))
	for i, c := range h {
		if t.excludeCol(c, wide) {
			continue
		}
		keep = append(keep, i)
		s.Columns = append(s.Columns, Column{Name: c.Name, AlignRight: c.Align == tview.AlignRight})
	}
	s.Rows = make([]Row, 0, data.RowCount())
	data.RowsRange(func(_ int, re model1.RowEvent) bool {
		r := Row{ID: re.Row.ID, Cells: make([]string, len(keep))}
		for j, ci := range keep {
			if ci >= len(re.Row.Fields) {
				continue
			}
			f := re.Row.Fields[ci]
			if h[ci].Decorator != nil {
				f = h[ci].Decorator(f)
			}
			r.Cells[j] = stripTags(f)
		}
		r.State = stateByColor[t.colorer(ns, h, &re)]
		s.Rows = append(s.Rows, r)
		return true
	})
	dropEmptyGPUCols(&s)

	return &s
}

// dropEmptyGPUCols hides the GPU columns when no row has a GPU value:
// on clusters without GPUs they are only noise eating table width.
func dropEmptyGPUCols(s *Snapshot) {
	keep := make([]int, 0, len(s.Columns))
	for i, c := range s.Columns {
		if !strings.Contains(c.Name, "GPU") || slices.ContainsFunc(s.Rows, func(r Row) bool { return !emptyGPU(r.Cells[i]) }) {
			keep = append(keep, i)
		}
	}
	if len(keep) == len(s.Columns) {
		return
	}
	cols := make([]Column, len(keep))
	for j, i := range keep {
		cols[j] = s.Columns[i]
	}
	s.Columns = cols
	for ri := range s.Rows {
		cells := make([]string, len(keep))
		for j, i := range keep {
			cells[j] = s.Rows[ri].Cells[i]
		}
		s.Rows[ri].Cells = cells
	}
}

func emptyGPU(v string) bool {
	switch v {
	case "", "0", "0:0", "n/a":
		return true
	}
	return false
}

func (t *TableWatch) excludeCol(h model1.HeaderColumn, wide bool) bool {
	return h.Hide || (!wide && h.Wide) ||
		(h.Name == "NAMESPACE" && !client.IsAllNamespaces(t.ns)) ||
		(h.MX && !t.cluster.HasMetrics()) ||
		h.VS
}

func defaultSortCol(h model1.Header) string {
	for _, n := range []string{"NAMESPACE", "NAME"} {
		if _, ok := h.IndexOf(n, true); ok {
			return n
		}
	}
	if len(h) > 0 {
		return h[0].Name
	}
	return ""
}

var tagRx = regexp.MustCompile(`\[[a-zA-Z0-9#]*:[a-zA-Z0-9#]*:[a-zA-Z]*\]|\[::\]|\[-:-:-\]`)

func stripTags(s string) string {
	if !strings.Contains(s, "[") {
		return s
	}
	return strings.TrimSpace(tagRx.ReplaceAllString(s, ""))
}

// Accessor returns a fresh (unshared) k9s DAO for a gvr, initialized.
func (c *Cluster) Accessor(gvr string) dao.Accessor {
	g := client.NewGVR(gvr)
	var a dao.Accessor
	if meta, ok := model.Registry[g]; ok && meta.DAO != nil {
		a = cloneAccessor(meta.DAO)
	} else if ok {
		a = new(dao.Resource)
	} else {
		a = new(dao.Table)
	}
	a.Init(c.factory, g)

	return a
}
