package ui

import (
	"context"
	"fmt"
	"image"
	"sort"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/giovanni/k9desk/internal/backend"
	"k8s.io/apimachinery/pkg/util/duration"
)

const overviewGVR = "overview"

// OverviewView is the Lens-like cluster dashboard.
type OverviewView struct {
	app    *App
	data   *backend.Overview
	cancel context.CancelFunc
	list   widget.List
	ns     string
	cards4 [4]widget.Clickable
	warnCl []widget.Clickable
}

func newOverview(a *App) *OverviewView {
	o := OverviewView{app: a}
	o.list.Axis = layout.Vertical
	return &o
}

func (o *OverviewView) open(ns string) {
	o.close()
	o.ns = ns
	ctx, cancel := context.WithCancel(context.Background())
	o.cancel = cancel
	c := o.app.cluster
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			d := c.Overview(ns)
			o.app.post(func() {
				if ctx.Err() == nil {
					o.data = &d
				}
			})
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func (o *OverviewView) close() {
	if o.cancel != nil {
		o.cancel()
		o.cancel = nil
	}
}

func (o *OverviewView) Layout(gtx C) D {
	th := o.app.th
	fill(gtx, pal.Bg)
	d := o.data
	if d == nil {
		return layout.Center.Layout(gtx, func(gtx C) D {
			return lbl(gtx, th, fontSans, 14, pal.TextDim, "Loading overview…")
		})
	}
	sections := []layout.Widget{
		func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return rbg(gtx, withAlpha(pal.Accent, 0x30), 7, func(gtx C) D {
						return inset(6).Layout(gtx, func(gtx C) D { return mdi(gtx, icDashboard, pal.Accent, 20) })
					})
				}),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 20, pal.TextBright, "Cluster overview") }),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Rigid(func(gtx C) D { return badge(gtx, th, "namespace: "+nsLabel(o.ns), pal.Text, pal.Panel) }),
			)
		},
		func(gtx C) D { return o.cards(gtx, d) },
		func(gtx C) D { return o.capacity(gtx, d) },
		func(gtx C) D { return o.phases(gtx, d) },
		func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return iconText(gtx, th, icWarning, 15, pal.TextBright, "Warnings") }),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D { return badge(gtx, th, itoa(len(d.Warnings)), pal.Warn, withAlpha(pal.Warn, 0x30)) }),
			)
		},
	}
	n := len(sections) + len(d.Warnings)
	return inset(20).Layout(gtx, func(gtx C) D {
		return material.List(th, &o.list).Layout(gtx, n, func(gtx C, i int) D {
			if i < len(sections) {
				return layout.Inset{Bottom: 16}.Layout(gtx, sections[i])
			}
			return o.warning(gtx, i-len(sections), &d.Warnings[i-len(sections)])
		})
	})
}

func card(gtx C, th *material.Theme, cl *widget.Clickable, ic *widget.Icon, title, value, sub string, accent colorNRGBA) D {
	return cl.Layout(gtx, func(gtx C) D {
		border := pal.Panel
		if cl.Hovered() {
			border = withAlpha(accent, 0xb0)
		}
		return rborder(gtx, border, pal.Panel, 10, func(gtx C) D {
			return cardBody(gtx, th, cl, ic, title, value, sub, accent)
		})
	})
}

func cardBody(gtx C, th *material.Theme, cl *widget.Clickable, ic *widget.Icon, title, value, sub string, accent colorNRGBA) D {
	return func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return inset(16).Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 11, pal.TextDim, title) }),
						layout.Rigid(layout.Spacer{Height: 6}.Layout),
						layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 28, pal.TextBright, value) }),
						layout.Rigid(func(gtx C) D {
							if cl.Hovered() {
								return iconText(gtx, th, icCollapse, 12, pal.Accent, "View all")
							}
							return lbl(gtx, th, fontSans, 12, accent, sub)
						}),
					)
				}),
				layout.Rigid(func(gtx C) D {
					return rbg(gtx, withAlpha(accent, 0x28), 22, func(gtx C) D {
						return inset(11).Layout(gtx, func(gtx C) D { return mdi(gtx, ic, accent, 22) })
					})
				}),
			)
		})
	}(gtx)
}

func (o *OverviewView) cards(gtx C, d *backend.Overview) D {
	th := o.app.th
	nodeC := pal.Ok
	if d.NodesReady < d.Nodes {
		nodeC = pal.Warn
	}
	depC := pal.Ok
	if d.DeploymentsBad > 0 {
		depC = pal.Err
	}
	bad := d.PodPhases["Failing"] + d.PodPhases["Failed"]
	podC := pal.Ok
	if bad > 0 {
		podC = pal.Err
	}
	targets := [4]string{"v1/nodes", "v1/pods", "apps/v1/deployments", "v1/namespaces"}
	for i := range o.cards4 {
		if o.cards4[i].Clicked(gtx) {
			o.app.openResource(targets[i])
			return D{}
		}
	}
	items := []layout.Widget{
		func(gtx C) D {
			return card(gtx, th, &o.cards4[0], icNode, "NODES", fmt.Sprintf("%d / %d", d.NodesReady, d.Nodes), "ready", nodeC)
		},
		func(gtx C) D {
			return card(gtx, th, &o.cards4[1], icPod, "PODS", itoa(d.Pods), fmt.Sprintf("%d failing", bad), podC)
		},
		func(gtx C) D {
			return card(gtx, th, &o.cards4[2], icDeploy, "DEPLOYMENTS", itoa(d.Deployments), fmt.Sprintf("%d unavailable", d.DeploymentsBad), depC)
		},
		func(gtx C) D {
			return card(gtx, th, &o.cards4[3], icNamespace, "NAMESPACES", itoa(d.Namespaces), "total", pal.Info)
		},
	}
	var ch []layout.FlexChild
	for i, w := range items {
		if i > 0 {
			ch = append(ch, layout.Rigid(layout.Spacer{Width: 12}.Layout))
		}
		ch = append(ch, layout.Flexed(1, w))
	}
	return layout.Flex{}.Layout(gtx, ch...)
}

func (o *OverviewView) capacity(gtx C, d *backend.Overview) D {
	th := o.app.th
	cpu := func(m int64) string { return fmt.Sprintf("%.2f", float64(m)/1000) }
	mem := func(b int64) string { return fmt.Sprintf("%.1fGi", float64(b)/(1<<30)) }
	return layout.Flex{}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D {
			return meter(gtx, th, icCPU, "CPU (cores)", d.CPURequests, d.CPUUsage, d.CPUCapacity, cpu)
		}),
		layout.Rigid(layout.Spacer{Width: 12}.Layout),
		layout.Flexed(1, func(gtx C) D {
			return meter(gtx, th, icMem, "Memory", d.MemRequests, d.MemUsage, d.MemCapacity, mem)
		}),
	)
}

// meter shows requests (and usage when metrics-server is present) vs allocatable.
func meter(gtx C, th *material.Theme, ic *widget.Icon, title string, req, usage, capacity int64, f func(int64) string) D {
	return rbg(gtx, pal.Panel, 10, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return inset(16).Layout(gtx, func(gtx C) D {
			bar := func(gtx C, v int64, c colorNRGBA, label string) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return lbl(gtx, th, fontSans, 12, pal.Text, label)
					}),
					layout.Rigid(layout.Spacer{Height: 4}.Layout),
					layout.Rigid(func(gtx C) D {
						w, h := gtx.Constraints.Max.X, gtx.Dp(8)
						fillRRect(gtx, pal.Bg, image.Rect(0, 0, w, h), h/2)
						if capacity > 0 && v > 0 {
							fw := max(h, int(float64(w)*min(1, float64(v)/float64(capacity))))
							fillRRect(gtx, c, image.Rect(0, 0, fw, h), h/2)
						}
						return D{Size: image.Pt(w, h)}
					}),
				)
			}
			pct := func(v int64) string {
				if capacity == 0 {
					return "0%"
				}
				return fmt.Sprintf("%.0f%%", 100*float64(v)/float64(capacity))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return mdi(gtx, ic, pal.Accent, 18) }),
						layout.Rigid(layout.Spacer{Width: 8}.Layout),
						layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 14, pal.TextBright, title) }),
						layout.Flexed(1, layout.Spacer{}.Layout),
						layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 12, pal.TextDim, "allocatable "+f(capacity)) }),
					)
				}),
				layout.Rigid(layout.Spacer{Height: 10}.Layout),
				layout.Rigid(func(gtx C) D {
					if usage < 0 {
						return lbl(gtx, th, fontSans, 12, pal.TextDim, "Usage: metrics-server not available")
					}
					return bar(gtx, usage, pal.Accent, "Usage "+f(usage)+" ("+pct(usage)+")")
				}),
				layout.Rigid(layout.Spacer{Height: 10}.Layout),
				layout.Rigid(func(gtx C) D {
					return bar(gtx, req, pal.Info, "Requests "+f(req)+" ("+pct(req)+")")
				}),
			)
		})
	})
}

var phaseColors = map[string]uint32{
	"Running": 0x4caf50, "Succeeded": 0x7d848c, "Pending": 0xff9800,
	"Failing": 0xe85555, "Failed": 0xb33a3a, "Terminating": 0xc792ea,
}

func (o *OverviewView) phases(gtx C, d *backend.Overview) D {
	th := o.app.th
	keys := make([]string, 0, len(d.PodPhases))
	for k := range d.PodPhases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return rbg(gtx, pal.Panel, 10, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return inset(16).Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return iconText(gtx, th, icPod, 14, pal.TextBright, "Pod status") }),
				layout.Rigid(layout.Spacer{Height: 10}.Layout),
				layout.Rigid(func(gtx C) D {
					w, h := gtx.Constraints.Max.X, gtx.Dp(12)
					cl := clip.UniformRRect(image.Rect(0, 0, w, h), h/2).Push(gtx.Ops)
					defer cl.Pop()
					fillRect(gtx, pal.Bg, image.Rect(0, 0, w, h))
					x := 0
					for _, k := range keys {
						if d.Pods == 0 {
							break
						}
						pw := w * d.PodPhases[k] / d.Pods
						fillRect(gtx, rgb(phaseColors[k]), image.Rect(x, 0, x+pw, h))
						x += pw
					}
					return D{Size: image.Pt(w, h)}
				}),
				layout.Rigid(layout.Spacer{Height: 10}.Layout),
				layout.Rigid(func(gtx C) D {
					var ch []layout.FlexChild
					for _, k := range keys {
						k := k
						ch = append(ch, layout.Rigid(func(gtx C) D {
							return layout.Inset{Right: unit.Dp(18)}.Layout(gtx, func(gtx C) D {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx C) D {
										return dot(gtx, rgb(phaseColors[k]), 9)
									}),
									layout.Rigid(layout.Spacer{Width: 6}.Layout),
									layout.Rigid(func(gtx C) D {
										return lbl(gtx, th, fontSans, 12, pal.Text, fmt.Sprintf("%s %d", k, d.PodPhases[k]))
									}),
								)
							})
						}))
					}
					return layout.Flex{}.Layout(gtx, ch...)
				}),
			)
		})
	})
}

// warningTarget maps "Kind/name" + namespace to a gvr and path.
func warningTarget(w *backend.EventInfo) (string, string, bool) {
	kind, name, ok := strings.Cut(w.Object, "/")
	if !ok {
		return "", "", false
	}
	gvr, ok := backend.LookupAlias(kind)
	if !ok {
		return "", "", false
	}
	if backend.IsNamespaced(gvr) && w.Namespace != "" {
		return gvr, w.Namespace + "/" + name, true
	}
	return gvr, name, true
}

func (o *OverviewView) warning(gtx C, i int, w *backend.EventInfo) D {
	th := o.app.th
	if len(o.warnCl) <= i {
		o.warnCl = append(o.warnCl, make([]widget.Clickable, i+1-len(o.warnCl))...)
	}
	cl := &o.warnCl[i]
	gvr, path, linkable := warningTarget(w)
	if linkable && cl.Clicked(gtx) {
		o.app.goTo(gvr, path)
	}
	return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx C) D {
		bgc := pal.Panel
		if linkable && cl.Hovered() {
			bgc = pal.Hover
		}
		return cl.Layout(gtx, func(gtx C) D {
			return rbg(gtx, bgc, 8, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return insetHV(14, 10).Layout(gtx, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D { return mdi(gtx, icWarning, pal.Warn, 15) }),
								layout.Rigid(layout.Spacer{Width: 8}.Layout),
								layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 13, pal.Warn, w.Reason) }),
								layout.Rigid(layout.Spacer{Width: 10}.Layout),
								layout.Flexed(1, func(gtx C) D {
									return lbl(gtx, th, fontSans, 13, pal.TextBright, w.Namespace+"  ·  "+w.Object)
								}),
								layout.Rigid(func(gtx C) D {
									age := duration.HumanDuration(time.Since(w.Last))
									return lbl(gtx, th, fontSans, 12, pal.TextDim, fmt.Sprintf("×%d  %s ago", w.Count, age))
								}),
							)
						}),
						layout.Rigid(func(gtx C) D {
							l := material.Body2(th, w.Message)
							l.Color = pal.Text
							l.MaxLines = 2
							return l.Layout(gtx)
						}),
					)
				})
			})
		})
	})
}
