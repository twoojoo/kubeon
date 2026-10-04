//go:build dev && integration

package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/giovanni/kubeon/internal/backend"
	"github.com/giovanni/kubeon/internal/safety"
)

// harness renders the real App offscreen against the disposable kind
// cluster and lets tests inject input and capture PNG screenshots.
type harness struct {
	t    *testing.T
	a    *App
	win  *headless.Window
	r    input.Router
	ops  op.Ops
	size image.Point
	dir  string
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	kc, err := safety.ResolveKubeconfig(filepath.Join("..", "..", ".dev", "kubeconfig"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kc); err != nil {
		t.Skip("dev cluster not running: scripts/dev-cluster.sh up")
	}
	names, _, err := backend.Contexts(kc)
	if err != nil || len(safety.FilterContexts(names)) != len(names) {
		t.Fatalf("refusing kubeconfig %s (contexts %v)", kc, names)
	}
	safety.IsolateCaches(kc)
	opts.Kubeconfig = kc

	size := image.Pt(1440, 900)
	win, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Skipf("headless gpu unavailable: %v", err)
	}
	h := harness{t: t, win: win, size: size, dir: filepath.Join("..", "..", ".dev", "shots")}
	_ = os.MkdirAll(h.dir, 0o755)
	h.a = newApp(opts, nil)
	h.a.start()
	t.Cleanup(func() {
		h.a.shutdown()
		win.Release()
	})
	return &h
}

func (h *harness) frame() {
	h.a.drain()
	h.ops.Reset()
	gtx := layout.Context{
		Ops:         &h.ops,
		Now:         time.Now(),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(h.size),
		Source:      h.r.Source(),
	}
	h.a.layout(gtx)
	h.r.Frame(&h.ops)
	if err := h.win.Frame(&h.ops); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) waitFor(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		h.frame()
		if cond() {
			h.frame()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.shot("timeout-" + strings.ReplaceAll(what, " ", "_"))
	h.t.Fatalf("timeout waiting for %s", what)
}

func (h *harness) settle(d time.Duration) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		h.frame()
		time.Sleep(30 * time.Millisecond)
	}
}

func (h *harness) events(ee ...event.Event) {
	h.frame()
	h.r.Queue(ee...)
	h.frame()
	h.frame()
}

func (h *harness) press(name key.Name, mods ...key.Modifiers) {
	var m key.Modifiers
	for _, x := range mods {
		m |= x
	}
	h.events(key.Event{Name: name, Modifiers: m, State: key.Press}, key.Event{Name: name, Modifiers: m, State: key.Release})
}

func (h *harness) typeText(s string) {
	h.events(key.EditEvent{Text: s})
}

func (h *harness) shot(name string) {
	img := image.NewRGBA(image.Rectangle{Max: h.size})
	if err := h.win.Screenshot(img); err != nil {
		h.t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(h.dir, name+".png"))
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		h.t.Fatal(err)
	}
}

func TestUIFlow(t *testing.T) {
	h := newHarness(t, Options{Namespace: "demo", Resource: "po"})
	a := h.a
	h.waitFor("pods", func() bool { return a.view.snap != nil && len(a.view.snap.Rows) >= 4 })
	h.shot("01-pods")

	// select a running web pod
	for i, r := range a.view.snap.Rows {
		if strings.HasPrefix(r.ID, "demo/web-") {
			a.view.setSel(i)
			break
		}
	}
	h.press("D")
	h.waitFor("describe", func() bool { return a.drawer.open && !a.drawer.loading })
	h.shot("02-describe")
	h.press("Y")
	h.settle(200 * time.Millisecond)
	h.shot("03-yaml")
	h.press(key.NameEscape)
	if a.drawer.open {
		t.Fatal("esc should close drawer")
	}

	h.press("L")
	h.waitFor("logs", func() bool {
		lv, ok := a.dock.tabs[len(a.dock.tabs)-1].(*LogView)
		return ok && lv.count() > 3
	})
	h.shot("04-logs")

	h.press("S")
	h.waitFor("shell picker", func() bool { _, ok := a.overlay.(*Picker); return ok })
	h.shot("05-container-picker")
	h.press(key.NameReturn)
	h.waitFor("shell", func() bool {
		tm, ok := a.dock.tabs[len(a.dock.tabs)-1].(*Terminal)
		return ok && tm.sess != nil
	})
	h.settle(time.Second)
	h.typeText("echo hello-from-kubeon && uname -a")
	h.press(key.NameReturn)
	h.settle(1500 * time.Millisecond)
	h.shot("06-shell")
	tm := a.dock.tabs[len(a.dock.tabs)-1].(*Terminal)
	screen := tm.vt.String()
	if !strings.Contains(screen, "hello-from-kubeon") {
		t.Fatalf("shell output missing:\n%s", screen)
	}

	// command palette → deployments
	h.r.Source().Execute(key.FocusCmd{})
	h.frame()
	h.press(":", key.ModShift)
	h.waitFor("palette", func() bool { _, ok := a.overlay.(*Picker); return ok })
	h.typeText("deploy")
	h.settle(100 * time.Millisecond)
	h.shot("07-palette")
	h.press(key.NameReturn)
	h.waitFor("deployments", func() bool {
		return a.view.gvr == "apps/v1/deployments" && a.view.snap != nil && len(a.view.snap.Rows) >= 2
	})
	h.shot("08-deployments")

	// filter
	h.press("/")
	h.typeText("crash")
	h.waitFor("filtered", func() bool { return a.view.snap != nil && len(a.view.snap.Rows) == 1 })
	h.shot("09-filter")
}

func TestUIOverviewAndStress(t *testing.T) {
	h := newHarness(t, Options{})
	a := h.a
	h.waitFor("overview", func() bool { return a.ovw.data != nil })
	h.shot("10-overview")

	// large table: all configmaps (2000+ with STRESS=1 fixtures)
	a.openResource("v1/configmaps")
	h.waitFor("configmaps", func() bool { return a.view.snap != nil && !a.view.snap.Loading && len(a.view.snap.Rows) > 0 })
	n := len(a.view.snap.Rows)
	start := time.Now()
	frames := 0
	for time.Since(start) < 2*time.Second {
		a.view.move(7)
		h.frame()
		frames++
	}
	el := time.Since(start)
	t.Logf("%d rows: %d frames in %v (%.1f ms/frame incl. GPU readback-free render)", n, frames, el, float64(el.Milliseconds())/float64(frames))
	h.shot("11-configmaps")
}

func f32pt(x, y float32) f32.Point { return f32.Pt(x, y) }

// gtxForTest returns a context usable for direct widget calls.
func (h *harness) gtxForTest() layout.Context {
	h.ops.Reset()
	return layout.Context{Ops: &h.ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(h.size), Source: h.r.Source()}
}
