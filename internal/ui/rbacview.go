package ui

import (
	"fmt"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/giovanni/kubeon/internal/backend"
)

const rbacPage = "rbac-explorer"

// RBACView explores subjects, their effective permissions, can-i checks and
// reverse "who can" queries.
type RBACView struct {
	app             *App
	idx             *backend.RBACIndex
	sel             int
	who             bool // reverse query mode
	filter          widget.Editor
	loadSeq, sarSeq int
	verb            widget.Editor
	res             widget.Editor
	ns              widget.Editor

	subList, ruleList widget.List
	subBtns           map[string]*widget.Clickable
	linkBtns          map[string]*widget.Clickable
	modeSub, modeWho  widget.Clickable
	checkBtn          widget.Clickable

	// results
	checked    bool
	localOK    bool
	localRules []backend.EffectiveRule
	sar        string // "", "…", "allowed: …", "denied: …", "error: …"
	whoRes     []backend.Subject
	rules      []backend.EffectiveRule
}

func newRBACView(a *App) *RBACView {
	v := &RBACView{app: a, sel: -1, subBtns: map[string]*widget.Clickable{}, linkBtns: map[string]*widget.Clickable{}}
	for _, e := range []*widget.Editor{&v.filter, &v.verb, &v.res, &v.ns} {
		e.SingleLine, e.Submit = true, true
	}
	v.verb.SetText("get")
	v.res.SetText("pods")
	v.subList.Axis, v.ruleList.Axis = layout.Vertical, layout.Vertical
	return v
}

func (v *RBACView) open(ns string) {
	v.ns.SetText(ns)
	c := v.app.cluster
	v.loadSeq++
	seq := v.loadSeq
	go func() {
		idx := c.RBAC()
		v.app.post(func() {
			if seq != v.loadSeq || c != v.app.cluster {
				return // superseded, or another context by now
			}
			// keep the selected subject (by identity, indexes shift)
			var prev *backend.Subject
			if v.idx != nil && v.sel >= 0 && v.sel < len(v.idx.Subjects) {
				p := v.idx.Subjects[v.sel]
				prev = &p
			}
			v.idx, v.sel, v.sar = idx, -1, ""
			for i, s := range idx.Subjects {
				if prev != nil && s == *prev {
					v.sel = i
				}
			}
			v.refreshRules()
		})
	}()
}

func (v *RBACView) subjects() []int {
	if v.idx == nil {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(v.filter.Text()))
	var out []int
	if v.who && v.checked {
		keys := map[string]bool{}
		for _, s := range v.whoRes {
			keys[s.Key()] = true
		}
		for i, s := range v.idx.Subjects {
			if keys[s.Key()] && (q == "" || strings.Contains(strings.ToLower(s.Display()+" "+s.Kind), q)) {
				out = append(out, i)
			}
		}
		return out
	}
	for i, s := range v.idx.Subjects {
		if q == "" || strings.Contains(strings.ToLower(s.Display()+" "+s.Kind), q) {
			out = append(out, i)
		}
	}
	return out
}

func (v *RBACView) selected() (backend.Subject, bool) {
	if v.idx == nil || v.sel < 0 || v.sel >= len(v.idx.Subjects) {
		return backend.Subject{}, false
	}
	return v.idx.Subjects[v.sel], true
}

func (v *RBACView) refreshRules() {
	v.rules = nil
	if s, ok := v.selected(); ok {
		v.rules = v.idx.Rules(s)
	}
}

func (v *RBACView) query() backend.Query {
	g, r, sub := backend.ParseResource(v.res.Text())
	return backend.Query{Verb: strings.TrimSpace(v.verb.Text()), Group: g, Resource: r, Subresource: sub, Namespace: strings.TrimSpace(v.ns.Text())}
}

func (v *RBACView) check() {
	if v.idx == nil {
		return
	}
	q := v.query()
	v.checked = true
	if v.who {
		v.whoRes = v.idx.WhoCan(q)
		return
	}
	s, ok := v.selected()
	if !ok {
		return
	}
	v.localOK, v.localRules = v.idx.Check(s, q)
	v.sar = "…"
	c := v.app.cluster
	v.sarSeq++
	seq := v.sarSeq
	go func() {
		allowed, reason, err := c.AccessReview(s, q)
		v.app.post(func() {
			if seq != v.sarSeq { // a newer check (or subject) replaced it
				return
			}
			switch {
			case err != nil:
				v.sar = "error: " + err.Error()
			case allowed:
				v.sar = "allowed: " + reason
			default:
				v.sar = "denied: " + reason
			}
		})
	}()
}

func subjectIcon(kind string) *widget.Icon {
	switch kind {
	case rbacv1.ServiceAccountKind:
		return icSA
	case rbacv1.GroupKind:
		return icAccess
	}
	return icPerson
}

func (v *RBACView) click(m map[string]*widget.Clickable, k string) *widget.Clickable {
	c := m[k]
	if c == nil {
		c = new(widget.Clickable)
		m[k] = c
	}
	return c
}

func (v *RBACView) Layout(gtx C) D {
	th := v.app.th
	if v.modeSub.Clicked(gtx) {
		v.who, v.checked = false, false
	}
	if v.modeWho.Clicked(gtx) {
		v.who, v.checked = true, false
	}
	if v.checkBtn.Clicked(gtx) {
		v.check()
	}
	for _, e := range []*widget.Editor{&v.verb, &v.res, &v.ns} {
		for {
			ev, ok := e.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok {
				v.check()
				gtx.Execute(key.FocusCmd{})
			}
		}
	}
	for {
		if _, ok := v.filter.Update(gtx); !ok {
			break
		}
	}
	fill(gtx, pal.Bg)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return bg(gtx, pal.Panel, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Inset{Left: 16, Right: 14, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return rbg(gtx, withAlpha(pal.Accent, 0x30), 7, func(gtx C) D {
								return inset(6).Layout(gtx, func(gtx C) D { return mdi(gtx, icAccess, pal.Accent, 20) })
							})
						}),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 18, pal.TextBright, "RBAC Explorer") }),
						layout.Rigid(layout.Spacer{Width: 14}.Layout),
						layout.Rigid(func(gtx C) D {
							seg := func(cl *widget.Clickable, ic *widget.Icon, txt string, on bool) layout.FlexChild {
								return layout.Rigid(func(gtx C) D {
									b, c := colorNRGBA{}, pal.TextDim
									if on {
										b, c = pal.Selected, pal.TextBright
									}
									return pillButton(gtx, th, cl, ic, txt, c, b)
								})
							}
							return rborder(gtx, pal.Border, pal.Bg, 7, func(gtx C) D {
								return inset(2).Layout(gtx, func(gtx C) D {
									return layout.Flex{}.Layout(gtx,
										seg(&v.modeSub, icPerson, "Subjects", !v.who),
										seg(&v.modeWho, icSearch, "Who can…", v.who),
									)
								})
							})
						}),
						layout.Flexed(1, layout.Spacer{}.Layout),
						layout.Rigid(func(gtx C) D {
							if v.idx == nil {
								return D{}
							}
							return lbl(gtx, th, fontSans, 11, pal.TextDim, fmt.Sprintf("%d subjects", len(v.idx.Subjects)))
						}),
					)
				})
			})
		}),
		layout.Rigid(func(gtx C) D { return hline(gtx, pal.Border) }),
		layout.Rigid(v.layoutQuery),
		layout.Rigid(func(gtx C) D { return hline(gtx, pal.Border) }),
		layout.Flexed(1, func(gtx C) D {
			if v.idx == nil {
				return layout.Center.Layout(gtx, func(gtx C) D { return lbl(gtx, th, fontSans, 14, pal.TextDim, "Loading RBAC…") })
			}
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(v.layoutSubjects),
				layout.Rigid(func(gtx C) D { return vline(gtx, pal.Border) }),
				layout.Flexed(1, v.layoutDetail),
			)
		}),
	)
}

// focused reports whether one of the page's text fields has focus.
func (v *RBACView) focused(gtx C) bool {
	for _, e := range []*widget.Editor{&v.filter, &v.verb, &v.res, &v.ns} {
		if gtx.Focused(e) {
			return true
		}
	}
	return false
}

func (v *RBACView) editor(gtx C, e *widget.Editor, label, hint string, w int) D {
	th := v.app.th
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 10.5, pal.TextDim, label) }),
		layout.Rigid(layout.Spacer{Height: 3}.Layout),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(unitDp(w)), gtx.Dp(unitDp(w))
			b := pal.Border
			if gtx.Focused(e) {
				b = pal.Accent
			}
			return rborder(gtx, b, pal.Bg, 6, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return insetHV(8, 5).Layout(gtx, func(gtx C) D {
					ed := material.Editor(th, e, hint)
					ed.Font, ed.TextSize, ed.Color, ed.HintColor = fontMono, 12.5, pal.TextBright, pal.TextDim
					return ed.Layout(gtx)
				})
			})
		}),
	)
}

func (v *RBACView) layoutQuery(gtx C) D {
	th := v.app.th
	return insetHV(16, 10).Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.End}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				txt := "Can the selected subject"
				if v.who {
					txt = "Who can"
				}
				return layout.Inset{Right: 10, Bottom: 6}.Layout(gtx, func(gtx C) D { return lbl(gtx, th, fontSans, 13, pal.Text, txt) })
			}),
			layout.Rigid(func(gtx C) D { return v.editor(gtx, &v.verb, "VERB", "get", 110) }),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D { return v.editor(gtx, &v.res, "RESOURCE", "pods, deploy, pods/log…", 200) }),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D { return v.editor(gtx, &v.ns, "NAMESPACE", "(cluster-wide)", 160) }),
			layout.Rigid(layout.Spacer{Width: 10}.Layout),
			layout.Rigid(func(gtx C) D {
				txt := "Check"
				if v.who {
					txt = "Find subjects"
				}
				return pillButton(gtx, th, &v.checkBtn, icSearch, txt, pal.OnAccent, pal.Accent)
			}),
			layout.Rigid(layout.Spacer{Width: 14}.Layout),
			layout.Flexed(1, func(gtx C) D {
				if !v.checked {
					return D{}
				}
				return layout.Inset{Bottom: 6}.Layout(gtx, v.layoutVerdict)
			}),
		)
	})
}

func (v *RBACView) layoutVerdict(gtx C) D {
	th := v.app.th
	if v.who {
		return iconText(gtx, th, icAccess, 13, pal.Text, fmt.Sprintf("%d subjects allowed (local evaluation)", len(v.whoRes)))
	}
	if _, ok := v.selected(); !ok {
		return lbl(gtx, th, fontSans, 12, pal.TextDim, "select a subject")
	}
	ic, c, txt := icClose, pal.Err, "denied"
	if v.localOK {
		ic, c, txt = icOK, pal.Ok, fmt.Sprintf("allowed by %d rule(s)", len(v.localRules))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return iconText(gtx, th, ic, 13, c, "Local RBAC: "+txt) }),
		layout.Rigid(func(gtx C) D {
			sc := pal.TextDim
			switch {
			case strings.HasPrefix(v.sar, "allowed"):
				sc = pal.Ok
			case strings.HasPrefix(v.sar, "denied"), strings.HasPrefix(v.sar, "error"):
				sc = pal.Err
			}
			return lbl(gtx, th, fontSans, 12, sc, "API server: "+strings.TrimSuffix(v.sar, ": "))
		}),
	)
}

func (v *RBACView) layoutSubjects(gtx C) D {
	th := v.app.th
	w := gtx.Dp(320)
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
	ids := v.subjects()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return inset(10).Layout(gtx, func(gtx C) D {
				return rborder(gtx, pal.Border, pal.Bg, 6, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return insetHV(8, 5).Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return mdi(gtx, icSearch, pal.TextDim, 14) }),
							layout.Rigid(layout.Spacer{Width: 6}.Layout),
							layout.Flexed(1, func(gtx C) D {
								e := material.Editor(th, &v.filter, "Filter subjects")
								e.TextSize, e.HintColor = 12.5, pal.TextDim
								return e.Layout(gtx)
							}),
						)
					})
				})
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			return material.List(th, &v.subList).Layout(gtx, len(ids), func(gtx C, i int) D {
				s := v.idx.Subjects[ids[i]]
				cl := v.click(v.subBtns, s.Key())
				if cl.Clicked(gtx) {
					v.sel = ids[i]
					v.sarSeq++ // a pending review is about the previous subject
					v.sar = ""
					v.refreshRules()
					if v.who {
						v.who, v.checked = false, false
					} else {
						v.checked = false
					}
				}
				return cl.Layout(gtx, func(gtx C) D {
					b := colorNRGBA{}
					switch {
					case ids[i] == v.sel:
						b = pal.Selected
					case cl.Hovered():
						b = pal.Hover
					}
					return bg(gtx, b, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return insetHV(12, 6).Layout(gtx, func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D { return mdi(gtx, subjectIcon(s.Kind), pal.Accent, 15) }),
								layout.Rigid(layout.Spacer{Width: 8}.Layout),
								layout.Flexed(1, func(gtx C) D { return lbl(gtx, th, fontSans, 12.5, pal.TextBright, s.Display()) }),
								layout.Rigid(func(gtx C) D {
									return lbl(gtx, th, fontSans, 10.5, pal.TextDim, fmt.Sprintf("%s · %d", strings.ToLower(s.Kind), s.Bindings))
								}),
							)
						})
					})
				})
			})
		}),
	)
}

func (v *RBACView) layoutDetail(gtx C) D {
	th := v.app.th
	s, ok := v.selected()
	if !ok {
		return layout.Center.Layout(gtx, func(gtx C) D {
			msg := "Select a subject to see its effective permissions"
			if v.who {
				msg = "Ask “who can …” above, then pick a subject"
			}
			return iconText(gtx, th, icAccess, 13, pal.TextDim, msg)
		})
	}
	rules := v.rules
	if v.checked && !v.who {
		rules = v.localRules
	}
	return inset(14).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return mdi(gtx, subjectIcon(s.Kind), pal.Accent, 20) }),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 16, pal.TextBright, s.Display()) }),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D { return badge(gtx, th, s.Kind, pal.Text, pal.Panel) }),
					layout.Flexed(1, layout.Spacer{}.Layout),
					layout.Rigid(func(gtx C) D {
						t := fmt.Sprintf("%d effective rules", len(v.rules))
						if v.checked && !v.who {
							t = fmt.Sprintf("%d rules match the query", len(rules))
						}
						return lbl(gtx, th, fontSans, 11, pal.TextDim, t)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: 10}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(1.2, func(gtx C) D { return lbl(gtx, th, fontSans, 10.5, pal.TextDim, "SCOPE") }),
					layout.Flexed(1.6, func(gtx C) D { return lbl(gtx, th, fontSans, 10.5, pal.TextDim, "VERBS") }),
					layout.Flexed(3, func(gtx C) D { return lbl(gtx, th, fontSans, 10.5, pal.TextDim, "RESOURCES") }),
					layout.Flexed(2.6, func(gtx C) D { return lbl(gtx, th, fontSans, 10.5, pal.TextDim, "GRANTED BY") }),
				)
			}),
			layout.Rigid(func(gtx C) D { return hline(gtx, pal.Border) }),
			layout.Flexed(1, func(gtx C) D {
				if len(rules) == 0 {
					return inset(16).Layout(gtx, func(gtx C) D {
						return lbl(gtx, th, fontSans, 12.5, pal.TextDim, "No permissions granted (besides implicit discovery).")
					})
				}
				return material.List(th, &v.ruleList).Layout(gtx, len(rules), func(gtx C, i int) D {
					return v.layoutRule(gtx, i, rules[i])
				})
			}),
		)
	})
}

func (v *RBACView) layoutRule(gtx C, i int, er backend.EffectiveRule) D {
	th := v.app.th
	verbs, res := backend.RuleString(er.Rule)
	scope := er.Namespace
	if scope == "" {
		scope = "cluster-wide"
	}
	link := func(key string, l backend.Link, txt string) layout.Widget {
		return func(gtx C) D {
			cl := v.click(v.linkBtns, key)
			if cl.Clicked(gtx) {
				v.app.followLink(&l)
			}
			return cl.Layout(gtx, func(gtx C) D {
				c := pal.Accent
				if cl.Hovered() {
					c = lighten(pal.Accent, 0.25)
				}
				return lbl(gtx, th, fontSans, 12, c, txt)
			})
		}
	}
	verbC := pal.Text
	if strings.Contains(verbs, "*") || strings.Contains(verbs, "delete") || strings.Contains(verbs, "escalate") || strings.Contains(verbs, "impersonate") {
		verbC = pal.Warn
	}
	return layout.Inset{Top: 5, Bottom: 5}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1.2, func(gtx C) D { return lbl(gtx, th, fontSans, 12, pal.TextDim, scope) }),
			layout.Flexed(1.6, func(gtx C) D { return lbl(gtx, th, fontMono, 11.5, verbC, verbs) }),
			layout.Flexed(3, func(gtx C) D {
				l := material.Body2(th, res)
				l.Font, l.TextSize, l.Color, l.MaxLines = fontMono, 11.5, pal.Text, 3
				return layout.Inset{Right: 8}.Layout(gtx, l.Layout)
			}),
			layout.Flexed(2.6, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(link(fmt.Sprintf("r%d", i), er.Role, shortName(er.Role.Path))),
					layout.Rigid(link(fmt.Sprintf("b%d", i), er.Binding, "via "+shortName(er.Binding.Path))),
					layout.Rigid(func(gtx C) D {
						if er.ViaGroup == "" {
							return D{}
						}
						return lbl(gtx, th, fontSans, 10.5, pal.TextDim, "as member of "+er.ViaGroup)
					}),
				)
			}),
		)
	})
}
