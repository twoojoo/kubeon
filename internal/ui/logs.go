package ui

import (
	"context"
	"image"
	"io"
	"regexp"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/giovanni/k9desk/internal/backend"
)

const maxLogLines = 200_000

// LogView streams logs (k9s dao TailLogs) into a virtualized list.
type LogView struct {
	app       *App
	gvr, path string
	previous  bool
	cancel    context.CancelFunc

	lines    []backend.LogLine
	multi    bool
	filtered []int // indices into lines when filtering
	rx       *regexp.Regexp
	rawQ     string
	err      string

	list   widget.List
	filter widget.Editor

	wrap, timestamps, allContainers bool

	followBtn, wrapBtn, tsBtn, clearBtn, copyBtn, allBtn widget.Clickable
}

func newLogView(a *App, gvr, path string, previous bool) *LogView {
	l := LogView{app: a, gvr: gvr, path: path, previous: previous, wrap: true, allContainers: true}
	l.list.Axis = layout.Vertical
	l.list.ScrollToEnd = true
	l.filter.SingleLine = true
	l.start()
	return &l
}

func (l *LogView) Title() string {
	t := "Logs " + shortName(l.path)
	if l.previous {
		t += " (prev)"
	}
	return t
}

func (l *LogView) Icon() *widget.Icon { return icLogs }

func (l *LogView) Focused(gtx C) bool { return gtx.Focused(&l.filter) }

func (l *LogView) Close() {
	if l.cancel != nil {
		l.cancel()
	}
}

func (l *LogView) start() {
	l.Close()
	l.lines, l.filtered, l.err = l.lines[:0], nil, ""
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	c := l.app.cluster
	opts := backend.LogOpts{AllContainers: l.allContainers, Previous: l.previous, Timestamps: l.timestamps, TailLines: 1000}
	go func() {
		err := c.TailLogs(ctx, l.gvr, l.path, opts, func(batch []backend.LogLine) {
			l.app.post(func() {
				if ctx.Err() == nil {
					l.append(batch)
				}
			})
		})
		if err != nil {
			l.app.post(func() { l.err = err.Error() })
		}
	}()
}

func (l *LogView) append(batch []backend.LogLine) {
	base := len(l.lines)
	l.lines = append(l.lines, batch...)
	for i := range batch {
		if batch[i].Container != "" && len(l.lines) > 0 && batch[i].Container != l.lines[0].Container {
			l.multi = true
		}
	}
	if l.rx != nil {
		for i := range batch {
			if l.rx.MatchString(batch[i].Text) {
				l.filtered = append(l.filtered, base+i)
			}
		}
	}
	if over := len(l.lines) - maxLogLines; over > 0 {
		drop := over + maxLogLines/10
		l.lines = append(l.lines[:0:0], l.lines[drop:]...)
		l.rebuildFilter()
	}
}

func (l *LogView) setFilter(q string) {
	if q == l.rawQ {
		return
	}
	l.rawQ = q
	l.rx = nil
	if q != "" {
		rx, err := regexp.Compile("(?i)" + q)
		if err != nil {
			rx = regexp.MustCompile("(?i)" + regexp.QuoteMeta(q))
		}
		l.rx = rx
	}
	l.rebuildFilter()
}

func (l *LogView) rebuildFilter() {
	l.filtered = l.filtered[:0]
	if l.rx == nil {
		return
	}
	for i := range l.lines {
		if l.rx.MatchString(l.lines[i].Text) {
			l.filtered = append(l.filtered, i)
		}
	}
}

func (l *LogView) count() int {
	if l.rx != nil {
		return len(l.filtered)
	}
	return len(l.lines)
}

func (l *LogView) at(i int) *backend.LogLine {
	if l.rx != nil {
		return &l.lines[l.filtered[i]]
	}
	return &l.lines[i]
}

func (l *LogView) following() bool { return !l.list.Position.BeforeEnd }

func (l *LogView) Layout(gtx C) D {
	th := l.app.th
	for {
		ev, ok := l.filter.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok {
			l.setFilter(strings.TrimSpace(l.filter.Text()))
		}
	}
	if l.followBtn.Clicked(gtx) {
		l.list.Position.BeforeEnd = false
		l.list.Position.First = l.count()
	}
	if l.wrapBtn.Clicked(gtx) {
		l.wrap = !l.wrap
	}
	if l.tsBtn.Clicked(gtx) {
		l.timestamps = !l.timestamps
		l.start()
	}
	if l.allBtn.Clicked(gtx) {
		l.allContainers = !l.allContainers
		l.start()
	}
	if l.clearBtn.Clicked(gtx) {
		l.lines, l.filtered = l.lines[:0], l.filtered[:0]
	}
	if l.copyBtn.Clicked(gtx) {
		var b strings.Builder
		for i := 0; i < l.count(); i++ {
			b.WriteString(l.at(i).Text)
			b.WriteByte('\n')
		}
		gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(b.String()))})
		l.app.flash("Logs copied to clipboard", false)
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return insetHV(8, 4).Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Max.X = gtx.Dp(260)
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						b := pal.Border
						if gtx.Focused(&l.filter) {
							b = pal.Accent
						}
						return rborder(gtx, b, pal.Bg, 6, func(gtx C) D {
							return insetHV(8, 5).Layout(gtx, func(gtx C) D {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx C) D { return mdi(gtx, icSearch, pal.TextDim, 14) }),
									layout.Rigid(layout.Spacer{Width: 6}.Layout),
									layout.Flexed(1, func(gtx C) D {
										e := material.Editor(th, &l.filter, "Search logs (regex)")
										e.TextSize = 12
										e.HintColor = pal.TextDim
										return e.Layout(gtx)
									}),
								)
							})
						})
					}),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D {
						return toggle(gtx, th, &l.followBtn, "Follow", l.following())
					}),
					layout.Rigid(func(gtx C) D { return toggle(gtx, th, &l.wrapBtn, "Wrap", l.wrap) }),
					layout.Rigid(func(gtx C) D { return toggle(gtx, th, &l.tsBtn, "Timestamps", l.timestamps) }),
					layout.Rigid(func(gtx C) D { return toggle(gtx, th, &l.allBtn, "All containers", l.allContainers) }),
					layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &l.clearBtn, icClear, "Clear", pal.TextDim, colorNRGBA{}) }),
					layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &l.copyBtn, icCopy, "Copy", pal.TextDim, colorNRGBA{}) }),
					layout.Flexed(1, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return layout.E.Layout(gtx, func(gtx C) D {
							msg := itoa(l.count()) + " lines"
							c := pal.TextDim
							if l.err != "" {
								msg, c = l.err, pal.Err
							}
							return lbl(gtx, th, fontSans, 12, c, msg)
						})
					}),
				)
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			fill(gtx, rgb(0x16191b))
			return layout.Inset{Left: 8, Right: 4}.Layout(gtx, func(gtx C) D {
				return material.List(th, &l.list).Layout(gtx, l.count(), func(gtx C, i int) D {
					return l.layoutLine(gtx, l.at(i))
				})
			})
		}),
	)
}

func (l *LogView) layoutLine(gtx C, ln *backend.LogLine) D {
	th := l.app.th
	c := pal.Text
	if ln.IsError {
		c = pal.Err
	}
	lw := widget.Label{}
	if !l.wrap {
		lw.MaxLines = 1
		lw.Truncator = "…"
	}
	txt := ln.Text
	if l.multi && ln.Container != "" {
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return lbl(gtx, th, fontMono, 12, containerColor(ln.Container), ln.Container+" ")
			}),
			layout.Flexed(1, func(gtx C) D {
				return lw.Layout(gtx, th.Shaper, fontMono, 12, txt, colorMat(gtx, c))
			}),
		)
	}
	return lw.Layout(gtx, th.Shaper, fontMono, 12, txt, colorMat(gtx, c))
}

var containerPalette = []uint32{0x5fb3e8, 0xc792ea, 0x89ddff, 0xffcb6b, 0xc3e88d, 0xf78c6c}

func containerColor(name string) colorNRGBA {
	h := 0
	for _, r := range name {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return rgb(containerPalette[h%len(containerPalette)])
}

// toggle is a small switch with a label.
func toggle(gtx C, th *material.Theme, cl *widget.Clickable, txt string, on bool) D {
	return cl.Layout(gtx, func(gtx C) D {
		return insetHV(7, 4).Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					w, h := gtx.Dp(26), gtx.Dp(14)
					track := pal.Border
					if on {
						track = pal.Accent
					}
					fillRRect(gtx, track, image.Rect(0, 0, w, h), h/2)
					k := h - gtx.Dp(4)
					x := gtx.Dp(2)
					if on {
						x = w - k - gtx.Dp(2)
					}
					fillRRect(gtx, pal.TextBright, image.Rect(x, gtx.Dp(2), x+k, gtx.Dp(2)+k), k/2)
					return D{Size: image.Pt(w, h)}
				}),
				layout.Rigid(layout.Spacer{Width: 6}.Layout),
				layout.Rigid(func(gtx C) D {
					c := pal.TextDim
					if on || cl.Hovered() {
						c = pal.Text
					}
					return lbl(gtx, th, fontSans, 12, c, txt)
				}),
			)
		})
	})
}

func shortName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
