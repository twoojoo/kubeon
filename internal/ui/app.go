// Package ui is the native Gio front-end of k9desk.
package ui

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/giovanni/k9desk/internal/backend"
	"github.com/giovanni/k9desk/internal/safety"
)

// Options configure the UI.
type Options struct {
	Kubeconfig string
	Context    string
	Namespace  string
	Resource   string
}

// App is the root UI state. All fields are owned by the UI goroutine;
// background work hands results back through post.
type App struct {
	w    *app.Window
	th   *material.Theme
	opts Options

	inboxMx sync.Mutex
	inbox   []func()

	contexts   []string
	cluster    *backend.Cluster
	connecting string
	connErr    error
	namespaces []string
	ns         string // "" = all namespaces

	status    string
	statusErr bool
	statusAt  time.Time

	sidebar *Sidebar
	view    *ResourceView
	drawer  *Drawer
	dock    *Dock
	overlay overlay

	ctxBtn widget.Clickable

	onInvalidate func() // headless harness hook
}

// overlay is a modal layer (picker, confirm dialog).
type overlay interface {
	Layout(gtx C) D
	// Close reports whether the overlay asked to be dismissed.
	Done() bool
}

// Run opens the main window and blocks until it is closed.
func Run(opts Options) error {
	a := newApp(opts, new(app.Window))
	a.w.Option(app.Title("k9desk"), app.Size(unit.Dp(1440), unit.Dp(900)), app.MinSize(unit.Dp(800), unit.Dp(500)))
	a.start()

	errc := make(chan error, 1)
	go func() { errc <- a.loop() }()
	app.Main()
	return <-errc
}

func newApp(opts Options, w *app.Window) *App {
	a := App{opts: opts, th: newTheme(), ns: opts.Namespace, w: w}
	a.sidebar = newSidebar(&a)
	a.view = newResourceView(&a)
	a.drawer = newDrawer(&a)
	a.dock = newDock(&a)
	return &a
}

// start lists contexts and connects to the initial one.
func (a *App) start() {
	opts := a.opts
	names, current, err := backend.Contexts(opts.Kubeconfig)
	if err != nil {
		a.connErr = err
	}
	a.contexts = safety.FilterContexts(names)
	target := opts.Context
	if target == "" {
		target = current
	}
	if target != "" {
		a.connect(target)
	} else if err == nil {
		a.connErr = fmt.Errorf("no usable context in %s", opts.Kubeconfig)
	}
}

func (a *App) loop() error {
	var ops op.Ops
	for {
		switch e := a.w.Event().(type) {
		case app.DestroyEvent:
			a.shutdown()
			return e.Err
		case app.FrameEvent:
			a.drain()
			gtx := app.NewContext(&ops, e)
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// post schedules f on the UI goroutine and wakes the window.
func (a *App) post(f func()) {
	a.inboxMx.Lock()
	a.inbox = append(a.inbox, f)
	a.inboxMx.Unlock()
	a.invalidate()
}

func (a *App) drain() {
	a.inboxMx.Lock()
	q := a.inbox
	a.inbox = nil
	a.inboxMx.Unlock()
	for _, f := range q {
		f()
	}
}

func (a *App) shutdown() {
	a.view.close()
	a.dock.closeAll()
	if a.cluster != nil {
		a.cluster.Close()
	}
}

// flash shows a status message.
func (a *App) flash(msg string, isErr bool) {
	a.status, a.statusErr, a.statusAt = msg, isErr, time.Now()
	if isErr {
		slog.Warn(msg)
	}
	a.invalidate()
}

// do runs fn in background and flashes its outcome.
func (a *App) do(desc string, fn func() error) {
	a.flash(desc+"…", false)
	go func() {
		err := fn()
		a.post(func() {
			if err != nil {
				a.flash(fmt.Sprintf("%s failed: %v", desc, err), true)
				return
			}
			a.flash(desc+" ✓", false)
		})
	}()
}

func (a *App) connect(kctx string) {
	if err := safety.CheckContext(kctx); err != nil {
		a.connErr = err
		return
	}
	a.view.close()
	a.dock.closeAll()
	a.drawer.close()
	if a.cluster != nil {
		a.cluster.Close()
		a.cluster = nil
	}
	a.connecting, a.connErr = kctx, nil
	go func() {
		c, err := backend.Connect(a.opts.Kubeconfig, kctx)
		var nss []string
		if err == nil {
			nss, _ = c.Namespaces()
		}
		a.post(func() {
			if a.connecting != kctx {
				if c != nil {
					c.Close()
				}
				return
			}
			a.connecting = ""
			if err != nil {
				a.connErr = err
				return
			}
			a.cluster, a.namespaces = c, nss
			a.sidebar.reload(c)
			res := a.opts.Resource
			if res == "" {
				res = "v1/pods"
			}
			a.openResource(res)
			a.flash(fmt.Sprintf("Connected to %s (%s)", c.Context, c.Version()), false)
		})
	}()
}

func (a *App) refreshNamespaces() {
	c := a.cluster
	if c == nil {
		return
	}
	go func() {
		nss, err := c.Namespaces()
		if err == nil {
			a.post(func() { a.namespaces = nss })
		}
	}()
}

func (a *App) openResource(gvr string) {
	if a.cluster == nil {
		return
	}
	a.drawer.close()
	a.view.open(gvr, a.ns)
	a.sidebar.active = gvr
}

func (a *App) setNamespace(ns string) {
	a.ns = ns
	if a.view.gvr != "" {
		a.view.open(a.view.gvr, ns)
	}
}

// textFocused reports whether a text input currently owns the keyboard.
func (a *App) textFocused(gtx C) bool {
	if a.overlay != nil {
		return true
	}
	return gtx.Focused(&a.view.filter) || a.drawer.focused(gtx) || a.dock.focused(gtx)
}

func (a *App) layout(gtx C) {
	fill(gtx, pal.Bg)
	if a.overlay != nil && a.overlay.Done() {
		a.overlay = nil
		gtx.Execute(key.FocusCmd{})
	}
	a.handleKeys(gtx)

	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(a.sidebar.Layout),
				layout.Rigid(func(gtx C) D { return vline(gtx, pal.Border) }),
				layout.Flexed(1, a.layoutMain),
			)
		}),
		layout.Rigid(a.layoutStatus),
	)

	if a.overlay != nil {
		a.layoutModal(gtx)
	}
}

func (a *App) layoutModal(gtx C) {
	gtx.Constraints.Min = gtx.Constraints.Max
	// dim background and swallow pointer events
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, a.overlay)
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: a.overlay, Kinds: pointer.Press | pointer.Scroll, ScrollY: pointer.ScrollRange{Min: -1e6, Max: 1e6}}); !ok {
			break
		}
	}
	fill(gtx, withAlpha(rgb(0x000000), 0x90))
	layout.N.Layout(gtx, func(gtx C) D {
		return layout.Inset{Top: unit.Dp(90)}.Layout(gtx, a.overlay.Layout)
	})
}

func (a *App) layoutMain(gtx C) D {
	if a.cluster == nil {
		return a.layoutWelcome(gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D {
			if !a.drawer.open {
				return a.view.Layout(gtx)
			}
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(0.55, a.view.Layout),
				layout.Rigid(func(gtx C) D { return vline(gtx, pal.Border) }),
				layout.Flexed(0.45, a.drawer.Layout),
			)
		}),
		layout.Rigid(a.dock.Layout),
	)
}

func (a *App) layoutWelcome(gtx C) D {
	return layout.Center.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return lbl(gtx, a.th, fontSans, 28, pal.TextBright, "k9desk")
			}),
			layout.Rigid(layout.Spacer{Height: 12}.Layout),
			layout.Rigid(func(gtx C) D {
				switch {
				case a.connecting != "":
					return lbl(gtx, a.th, fontSans, 15, pal.Text, "Connecting to "+a.connecting+"…")
				case a.connErr != nil:
					gtx.Constraints.Max.X = gtx.Dp(700)
					l := material.Body1(a.th, a.connErr.Error())
					l.Color = pal.Err
					return l.Layout(gtx)
				default:
					return lbl(gtx, a.th, fontSans, 15, pal.Text, "Select a context")
				}
			}),
			layout.Rigid(layout.Spacer{Height: 16}.Layout),
			layout.Rigid(func(gtx C) D {
				if a.ctxBtn.Clicked(gtx) {
					a.pickContext()
				}
				if a.connecting != "" {
					return D{}
				}
				return button(gtx, a.th, &a.ctxBtn, "Choose context", pal.Accent)
			}),
		)
	})
}

func (a *App) layoutStatus(gtx C) D {
	h := gtx.Dp(24)
	gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = h, h
	fill(gtx, pal.Accent)
	return insetHV(10, 0).Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.Y = h
				return layout.W.Layout(gtx, func(gtx C) D {
					name := "no cluster"
					if a.cluster != nil {
						name = a.cluster.Context + "  ·  " + a.cluster.Version()
					}
					return lbl(gtx, a.th, fontSans, 12, pal.TextBright, name)
				})
			}),
			layout.Rigid(layout.Spacer{Width: 24}.Layout),
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.Y = h
				return layout.W.Layout(gtx, func(gtx C) D {
					if a.status == "" || time.Since(a.statusAt) > 8*time.Second && !a.statusErr {
						return D{}
					}
					c := pal.TextBright
					if a.statusErr {
						c = rgb(0xffd6d6)
					}
					return lbl(gtx, a.th, fontSans, 12, c, a.status)
				})
			}),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.Y = h
				return layout.E.Layout(gtx, func(gtx C) D {
					return lbl(gtx, a.th, fontSans, 12, pal.TextBright, ":cmd  /filter  enter details  l logs  s shell  y yaml  d describe  ^d delete")
				})
			}),
		)
	})
}

func (a *App) invalidate() {
	if a.w != nil {
		a.w.Invalidate()
	} else if a.onInvalidate != nil {
		a.onInvalidate()
	}
}
