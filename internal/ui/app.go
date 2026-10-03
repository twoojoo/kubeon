// Package ui is the native Gio front-end of k9desk.
package ui

import (
	"context"
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
	"github.com/giovanni/k9desk/internal/notify"
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

	sidebar  *Sidebar
	view     *ResourceView
	ovw      *OverviewView
	xray     *XRayView
	page     string // current gvr, overviewGVR or xrayPage
	navStack []navState
	drawer   *Drawer
	dock     *Dock
	overlay  overlay

	ctxBtn  widget.Clickable
	pfBtn   widget.Clickable
	bellBtn widget.Clickable

	events       []backend.HealthEvent
	unread       int
	notifyOn     bool
	notifier     *notify.Notifier
	healthCancel context.CancelFunc

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
	a := App{opts: opts, th: newTheme(), ns: opts.Namespace, w: w, notifyOn: true}
	if w != nil {
		a.notifier = notify.New("k9desk")
	}
	a.sidebar = newSidebar(&a)
	a.view = newResourceView(&a)
	a.ovw = newOverview(&a)
	a.xray = newXRay(&a)
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
	if a.healthCancel != nil {
		a.healthCancel()
	}
	a.view.close()
	a.ovw.close()
	a.xray.close()
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
			a.flash(desc+" — done", false)
		})
	}()
}

func (a *App) connect(kctx string) {
	if err := safety.CheckContext(kctx); err != nil {
		a.connErr = err
		return
	}
	a.view.close()
	a.ovw.close()
	a.xray.close()
	a.dock.closeAll()
	a.drawer.close()
	if a.cluster != nil {
		a.cluster.Close()
		a.cluster = nil
	}
	if a.healthCancel != nil {
		a.healthCancel()
		a.healthCancel = nil
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
			a.events, a.unread = nil, 0
			a.startHealth(c)
			a.sidebar.reload(c)
			res := a.opts.Resource
			if res == "" {
				res = overviewGVR
			} else if g, ok := backend.LookupAlias(res); ok {
				res = g
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

// openResource navigates from the sidebar/palette: resets history.
func (a *App) openResource(gvr string) {
	a.navStack = nil
	a.show(navState{page: gvr, gvr: gvr})
}

func (a *App) setNamespace(ns string) {
	a.ns = ns
	a.show(a.current())
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
			switch a.page {
			case overviewGVR:
				return a.ovw.Layout(gtx)
			case xrayPage:
				if a.drawer.open {
					return layout.Flex{}.Layout(gtx,
						layout.Flexed(0.55, a.xray.Layout),
						layout.Rigid(func(gtx C) D { return vline(gtx, pal.Border) }),
						layout.Flexed(0.45, a.drawer.Layout),
					)
				}
				return a.xray.Layout(gtx)
			}
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
				return rbg(gtx, withAlpha(pal.Accent, 0x30), 36, func(gtx C) D {
					return inset(18).Layout(gtx, func(gtx C) D {
						ic := icCluster
						switch {
						case a.connErr != nil:
							ic = icCloudOff
						case a.connecting != "":
							ic = icRestart
						}
						return mdi(gtx, ic, pal.Accent, 40)
					})
				})
			}),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(func(gtx C) D {
				return lbl(gtx, a.th, fontSans, 28, pal.TextBright, "k9desk")
			}),
			layout.Rigid(func(gtx C) D {
				return lbl(gtx, a.th, fontSans, 13, pal.TextDim, "native Kubernetes IDE · powered by k9s")
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
				return pillButton(gtx, a.th, &a.ctxBtn, icCluster, "Choose context", pal.TextBright, pal.Accent)
			}),
		)
	})
}

func (a *App) layoutStatus(gtx C) D {
	h := gtx.Dp(26)
	gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = h, h
	fill(gtx, pal.Accent)
	th := a.th
	fg := pal.TextBright
	center := func(w layout.Widget) layout.Widget {
		return func(gtx C) D {
			gtx.Constraints.Min.Y = h
			return layout.W.Layout(gtx, w)
		}
	}
	if a.pfBtn.Clicked(gtx) {
		a.pickForward()
	}
	if a.bellBtn.Clicked(gtx) {
		a.overlay = newNotifPanel(a)
	}
	return insetHV(10, 0).Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(center(func(gtx C) D {
				if a.cluster == nil {
					return iconText(gtx, th, icCloudOff, 12, fg, "no cluster")
				}
				return iconText(gtx, th, icCluster, 12, fg, a.cluster.Context)
			})),
			layout.Rigid(center(func(gtx C) D {
				if a.cluster == nil {
					return D{}
				}
				return layout.Inset{Left: 16}.Layout(gtx, func(gtx C) D {
					return iconText(gtx, th, icInfo, 12, fg, "Kubernetes "+a.cluster.Version())
				})
			})),
			layout.Rigid(center(func(gtx C) D {
				if a.cluster == nil {
					return D{}
				}
				n := len(a.cluster.PortForwards())
				if n == 0 {
					return D{}
				}
				return layout.Inset{Left: 16}.Layout(gtx, func(gtx C) D {
					return a.pfBtn.Layout(gtx, func(gtx C) D {
						return iconText(gtx, th, icForward, 12, fg, itoa(n)+" port-forward(s)")
					})
				})
			})),
			layout.Rigid(layout.Spacer{Width: 20}.Layout),
			layout.Flexed(1, center(func(gtx C) D {
				if a.status == "" || time.Since(a.statusAt) > 8*time.Second && !a.statusErr {
					return D{}
				}
				if a.statusErr {
					return rbg(gtx, pal.Err, 4, func(gtx C) D {
						return insetHV(6, 2).Layout(gtx, func(gtx C) D {
							return iconText(gtx, th, icError, 12, fg, a.status)
						})
					})
				}
				return iconText(gtx, th, icStatusDone, 12, fg, a.status)
			})),
			layout.Rigid(center(func(gtx C) D {
				if a.cluster == nil {
					return D{}
				}
				return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D {
					return a.bellBtn.Layout(gtx, func(gtx C) D {
						ic := icBell
						switch {
						case !a.notifyOn:
							ic = icBellOff
						case a.unread > 0:
							ic = icBellActive
						}
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return mdi(gtx, ic, fg, 16) }),
							layout.Rigid(func(gtx C) D {
								if a.unread == 0 {
									return D{}
								}
								return layout.Inset{Left: 3}.Layout(gtx, func(gtx C) D {
									return badge(gtx, th, itoa(a.unread), pal.TextBright, pal.Err)
								})
							}),
						)
					})
				})
			})),
			layout.Rigid(center(func(gtx C) D {
				hints := [][2]string{{":", "command"}, {"/", "filter"}, {"l", "logs"}, {"s", "shell"}, {"d", "describe"}, {"y", "yaml"}, {"^d", "delete"}}
				var ch []layout.FlexChild
				for _, k := range hints {
					k := k
					ch = append(ch, layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 10}.Layout(gtx, func(gtx C) D { return keyHint(gtx, th, k[0], k[1]) })
					}))
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, ch...)
			})),
		)
	})
}

// keyHint draws a keyboard chip followed by its meaning.
func keyHint(gtx C, th *material.Theme, k, what string) D {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return rbg(gtx, withAlpha(rgb(0x000000), 0x40), 3, func(gtx C) D {
				return insetHV(5, 1).Layout(gtx, func(gtx C) D {
					return lbl(gtx, th, fontMono, 11, pal.TextBright, k)
				})
			})
		}),
		layout.Rigid(layout.Spacer{Width: 4}.Layout),
		layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 11, withAlpha(pal.TextBright, 0xd0), what) }),
	)
}

func (a *App) invalidate() {
	if a.w != nil {
		a.w.Invalidate()
	} else if a.onInvalidate != nil {
		a.onInvalidate()
	}
}
