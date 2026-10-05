package ui

import (
	"fmt"
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"k8s.io/apimachinery/pkg/util/duration"

	"github.com/giovanni/kubeon/internal/backend"
)

const sanitizerPage = "sanitizer"

// SanitizerView shows the cluster lint report.
type SanitizerView struct {
	app     *App
	report  *backend.LintReport
	running bool
	ns      string
	sev     int    // 0 all, 1 errors, 2 warnings, 3 info
	cat     string // "" = all
	list    widget.List
	links   map[int]*widget.Clickable
	catBtns map[string]*widget.Clickable
	sevBtns [4]widget.Clickable
	runBtn  widget.Clickable
	askBtn  widget.Clickable
	seq     int
	cluster *backend.Cluster // the report's cluster
}

func newSanitizer(a *App) *SanitizerView {
	s := &SanitizerView{app: a, links: map[int]*widget.Clickable{}, catBtns: map[string]*widget.Clickable{}}
	s.list.Axis = layout.Vertical
	return s
}

func (s *SanitizerView) open(ns string) {
	if c := s.app.cluster; s.ns != ns || s.cluster != c {
		// never show a report of another namespace or cluster
		s.report, s.cluster = nil, c
	}
	s.ns = ns
	s.run()
}

// run starts a lint; a newer run supersedes a pending one.
func (s *SanitizerView) run() {
	c := s.app.cluster
	if c == nil {
		return
	}
	s.seq++
	s.running = true
	seq, ns := s.seq, s.ns
	go func() {
		r := c.Lint(ns)
		s.app.post(func() {
			if seq != s.seq || c != s.app.cluster {
				return
			}
			s.running = false
			s.report = r
			s.links = map[int]*widget.Clickable{}
		})
	}()
}

func sevStyleLint(sv backend.Severity) (*widget.Icon, colorNRGBA, string) {
	switch sv {
	case backend.SevErr:
		return icError, pal.Err, "error"
	case backend.SevWarn:
		return icWarning, pal.Warn, "warning"
	}
	return icInfo, pal.Info, "info"
}

func gradeColor(score int) colorNRGBA {
	switch {
	case score >= 80:
		return pal.Ok
	case score >= 60:
		return pal.Warn
	}
	return pal.Err
}

func (s *SanitizerView) visible() []int {
	if s.report == nil {
		return nil
	}
	var out []int
	for i, is := range s.report.Issues {
		if s.cat != "" && is.Category != s.cat {
			continue
		}
		switch s.sev {
		case 1:
			if is.Severity != backend.SevErr {
				continue
			}
		case 2:
			if is.Severity != backend.SevWarn {
				continue
			}
		case 3:
			if is.Severity != backend.SevInfo {
				continue
			}
		}
		out = append(out, i)
	}
	return out
}

func (s *SanitizerView) Layout(gtx C) D {
	a, th := s.app, s.app.th
	if s.runBtn.Clicked(gtx) {
		s.run()
	}
	if s.askBtn.Clicked(gtx) && s.report != nil {
		a.ai.open = true
		a.ai.send("Run the sanitizer (lint_cluster) for this namespace and tell me which findings matter most and how to fix them.")
	}
	for i := range s.sevBtns {
		if s.sevBtns[i].Clicked(gtx) {
			s.sev = i
		}
	}
	fill(gtx, pal.Bg)
	vis := s.visible()
	header := func(gtx C) D {
		return bg(gtx, pal.Panel, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Inset{Left: 16, Right: 14, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return rbg(gtx, withAlpha(pal.Accent, 0x30), 7, func(gtx C) D {
							return inset(6).Layout(gtx, func(gtx C) D { return mdi(gtx, icBug, pal.Accent, 20) })
						})
					}),
					layout.Rigid(layout.Spacer{Width: 10}.Layout),
					layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 18, pal.TextBright, "Sanitizer") }),
					layout.Rigid(layout.Spacer{Width: 10}.Layout),
					layout.Rigid(func(gtx C) D { return badge(gtx, th, "namespace: "+nsLabel(s.ns), pal.Text, pal.Bg) }),
					layout.Flexed(1, layout.Spacer{}.Layout),
					layout.Rigid(func(gtx C) D {
						if s.report == nil {
							return D{}
						}
						return layout.Inset{Right: 10}.Layout(gtx, func(gtx C) D {
							return lbl(gtx, th, fontSans, 11, pal.TextDim, "checked "+duration.HumanDuration(time.Since(s.report.At))+" ago")
						})
					}),
					layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &s.askBtn, icAI, "Ask assistant", pal.Text, colorNRGBA{}) }),
					layout.Rigid(func(gtx C) D {
						txt := "Run again"
						if s.running {
							txt = "Running…"
						}
						return pillButton(gtx, th, &s.runBtn, icRestart, txt, pal.Text, colorNRGBA{})
					}),
				)
			})
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(header),
		layout.Rigid(func(gtx C) D { return hline(gtx, pal.Border) }),
		layout.Flexed(1, func(gtx C) D {
			if s.report == nil {
				return layout.Center.Layout(gtx, func(gtx C) D {
					return lbl(gtx, th, fontSans, 14, pal.TextDim, "Checking the cluster…")
				})
			}
			extra := 3 // summary, filters, spacer
			return layout.Inset{Left: 16, Right: 16, Top: 14}.Layout(gtx, func(gtx C) D {
				return material.List(th, &s.list).Layout(gtx, len(vis)+extra, func(gtx C, i int) D {
					switch i {
					case 0:
						return layout.Inset{Bottom: 14}.Layout(gtx, s.layoutSummary)
					case 1:
						return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return s.layoutFilters(gtx, len(vis)) })
					case 2:
						if len(vis) == 0 {
							return inset(20).Layout(gtx, func(gtx C) D {
								return iconText(gtx, th, icOK, 13, pal.Ok, "No findings for this filter")
							})
						}
						return D{}
					}
					return s.layoutIssue(gtx, vis[i-extra])
				})
			})
		}),
	)
}

func (s *SanitizerView) layoutSummary(gtx C) D {
	th := s.app.th
	r := s.report
	gc := gradeColor(r.Score)
	var cards []layout.FlexChild
	cards = append(cards, layout.Rigid(func(gtx C) D {
		return rbg(gtx, pal.Panel, 10, func(gtx C) D {
			return inset(14).Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						sz := gtx.Dp(64)
						fillRRect(gtx, withAlpha(gc, 0x30), image.Rect(0, 0, sz, sz), sz/2)
						g := gtx
						g.Constraints = layout.Exact(image.Pt(sz, sz))
						layout.Center.Layout(g, func(gtx C) D { return lbl(gtx, th, fontSans, 30, gc, r.Grade()) })
						return D{Size: image.Pt(sz, sz)}
					}),
					layout.Rigid(layout.Spacer{Width: 12}.Layout),
					layout.Rigid(func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 24, pal.TextBright, fmt.Sprintf("%d%%", r.Score)) }),
							layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 11, pal.TextDim, "resources without errors or warnings") }),
						)
					}),
				)
			})
		})
	}))
	for _, cs := range r.Categories {
		if cs.Scanned == 0 && cs.Errors+cs.Warns+cs.Infos == 0 {
			continue
		}
		cs := cs
		cl := s.catBtns[cs.Name]
		if cl == nil {
			cl = new(widget.Clickable)
			s.catBtns[cs.Name] = cl
		}
		if cl.Clicked(gtx) {
			if s.cat == cs.Name {
				s.cat = ""
			} else {
				s.cat = cs.Name
			}
		}
		cards = append(cards, layout.Rigid(layout.Spacer{Width: 10}.Layout), layout.Flexed(1, func(gtx C) D {
			return cl.Layout(gtx, func(gtx C) D {
				border := pal.Panel
				if s.cat == cs.Name {
					border = pal.Accent
				} else if cl.Hovered() {
					border = pal.Border
				}
				return rborder(gtx, border, pal.Panel, 10, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return inset(12).Layout(gtx, func(gtx C) D {
						pc := cs.Percent()
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 11, pal.TextDim, upper(cs.Name)) }),
							layout.Rigid(layout.Spacer{Height: 4}.Layout),
							layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 18, gradeColor(pc), fmt.Sprintf("%d%%", pc)) }),
							layout.Rigid(layout.Spacer{Height: 6}.Layout),
							layout.Rigid(func(gtx C) D {
								w, h := gtx.Constraints.Max.X, gtx.Dp(5)
								fillRRect(gtx, pal.Bg, image.Rect(0, 0, w, h), h/2)
								fillRRect(gtx, gradeColor(pc), image.Rect(0, 0, max(h, w*pc/100), h), h/2)
								return D{Size: image.Pt(w, h)}
							}),
							layout.Rigid(layout.Spacer{Height: 6}.Layout),
							layout.Rigid(func(gtx C) D {
								return lbl(gtx, th, fontSans, 11, pal.TextDim, fmt.Sprintf("%d scanned · %dE %dW %dI", cs.Scanned, cs.Errors, cs.Warns, cs.Infos))
							}),
						)
					})
				})
			})
		}))
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, cards...)
}

func (s *SanitizerView) layoutFilters(gtx C, n int) D {
	th := s.app.th
	r := s.report
	var e, w, i int
	for _, is := range r.Issues {
		switch is.Severity {
		case backend.SevErr:
			e++
		case backend.SevWarn:
			w++
		default:
			i++
		}
	}
	labels := []string{fmt.Sprintf("All %d", len(r.Issues)), fmt.Sprintf("Errors %d", e), fmt.Sprintf("Warnings %d", w), fmt.Sprintf("Info %d", i)}
	icons := []*widget.Icon{icFilter, icError, icWarning, icInfo}
	var ch []layout.FlexChild
	for k := range labels {
		k := k
		ch = append(ch, layout.Rigid(func(gtx C) D {
			b, c := colorNRGBA{}, pal.TextDim
			if s.sev == k {
				b, c = pal.Selected, pal.TextBright
			}
			return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return pillButton(gtx, th, &s.sevBtns[k], icons[k], labels[k], c, b) })
		}))
	}
	if s.cat != "" {
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return badge(gtx, th, "category: "+s.cat, pal.OnAccent, pal.Accent) })
		}))
	}
	ch = append(ch, layout.Flexed(1, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.E.Layout(gtx, func(gtx C) D { return lbl(gtx, th, fontSans, 11, pal.TextDim, fmt.Sprintf("%d shown", n)) })
	}))
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, ch...)
}

func (s *SanitizerView) layoutIssue(gtx C, idx int) D {
	th := s.app.th
	if s.report == nil || idx >= len(s.report.Issues) {
		return D{}
	}
	is := s.report.Issues[idx]
	ic, c, _ := sevStyleLint(is.Severity)
	cl := s.links[idx]
	if cl == nil {
		cl = new(widget.Clickable)
		s.links[idx] = cl
	}
	if cl.Clicked(gtx) {
		s.app.followLink(&backend.Link{GVR: is.GVR, Path: is.Path})
	}
	return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D {
		return cl.Layout(gtx, func(gtx C) D {
			b := pal.Panel
			if cl.Hovered() {
				b = pal.Hover
			}
			return rbg(gtx, b, 7, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return insetHV(12, 8).Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return mdi(gtx, ic, c, 16) }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(func(gtx C) D {
							w := gtx.Dp(unit.Dp(300))
							gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D { return mdi(gtx, iconFor(is.GVR), pal.Accent, 14) }),
								layout.Rigid(layout.Spacer{Width: 6}.Layout),
								layout.Flexed(1, func(gtx C) D { return s.app.tipLbl(gtx, "san/"+is.Rule+"/"+is.Path+"/p", fontSans, 12.5, pal.Accent, is.Path) }),
							)
						}),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Flexed(1, func(gtx C) D { return s.app.tipLbl(gtx, "san/"+is.Rule+"/"+is.Path+"/m", fontSans, 12.5, pal.Text, is.Message) }),
						layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontMono, 10.5, pal.TextDim, is.Rule) }),
					)
				})
			})
		})
	})
}
