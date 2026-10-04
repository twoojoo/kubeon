package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/giovanni/k9desk/internal/backend"
)

const (
	tabDescribe = iota
	tabYAML
	tabData     // decoded secret data
	tabValues   // helm values
	tabOverview // structured details
	tabCount
)

var tabNames = [tabCount]string{"Describe", "YAML", "Data", "Values", "Overview"}

// Drawer is the right-hand details panel: describe, YAML (editable),
// decoded secret data, helm values.
type Drawer struct {
	app       *App
	open      bool
	gvr, path string
	tab       int
	editing   bool
	dirty     bool
	original  string
	loading   bool
	errMsg    string
	seq       int

	describe widget.Editor
	yaml     widget.Editor
	details  *backend.Details
	detV     *detailsView
	yamlV    *yamlView
	valV     *yamlView
	valMsg   string
	valLine  int
	valWarn  string
	values   widget.Editor

	secret     []backend.SecretEntry
	revealed   map[string]bool
	revealAll  bool
	secretList widget.List
	keyCopy    map[string]*widget.Clickable
	keyEye     map[string]*widget.Clickable
	allValues  bool

	tabBtns                               [tabCount]widget.Clickable
	closeBtn, editBtn, saveBtn, revertBtn widget.Clickable
	revealBtn, envBtn, dlBtn, allValBtn   widget.Clickable
}

func newDrawer(a *App) *Drawer {
	d := Drawer{app: a, revealed: map[string]bool{}, keyCopy: map[string]*widget.Clickable{}, keyEye: map[string]*widget.Clickable{}}
	d.detV = newDetailsView(a)
	d.yamlV, d.valV = newYAMLView(), newYAMLView()
	d.describe.ReadOnly = true
	d.yaml.ReadOnly = true
	d.values.ReadOnly = true
	d.secretList.Axis = layout.Vertical
	return &d
}

func isHelm(gvr string) bool { return gvr == backend.HelmGVR || gvr == backend.HelmHistoryGVR }

// tabs lists the tabs available for the current resource.
func (d *Drawer) tabs() []int {
	switch {
	case isHelm(d.gvr):
		return []int{tabDescribe, tabValues}
	case d.gvr == "v1/secrets":
		return []int{tabOverview, tabData, tabDescribe, tabYAML}
	case !strings.Contains(d.gvr, "/"):
		return []int{tabDescribe, tabYAML}
	default:
		return []int{tabOverview, tabDescribe, tabYAML}
	}
}

func (d *Drawer) hasTab(t int) bool {
	for _, x := range d.tabs() {
		if x == t {
			return true
		}
	}
	return false
}

func (d *Drawer) focused(gtx C) bool {
	return d.open && (gtx.Focused(&d.yaml) && d.editing)
}

func (d *Drawer) show(gvr, path string, tab int, edit bool) {
	d.open = true
	if d.gvr != gvr || d.path != path {
		d.gvr, d.path = gvr, path
		d.revealed, d.revealAll = map[string]bool{}, false
		d.load()
	}
	if !d.hasTab(tab) {
		tab = d.tabs()[0]
	}
	d.tab = tab
	d.setEditing(edit && d.hasTab(tabYAML))
}

// follow tracks table selection while not editing.
func (d *Drawer) follow(gvr, path string) {
	if d.editing && d.dirty {
		return
	}
	d.setEditing(false)
	if d.gvr != gvr || d.path != path {
		d.gvr, d.path = gvr, path
		d.revealed, d.revealAll = map[string]bool{}, false
		d.load()
		if !d.hasTab(d.tab) {
			d.tab = d.tabs()[0]
		}
	}
}

func (d *Drawer) close() {
	d.open, d.editing, d.dirty = false, false, false
	d.gvr, d.path = "", ""
	d.secret = nil
}

func (d *Drawer) setEditing(on bool) {
	d.editing = on
	d.valMsg, d.valLine, d.valWarn = "", 0, ""
	d.yaml.ReadOnly = !on
	if on {
		d.tab = tabYAML
	}
}

func (d *Drawer) load() {
	c := d.app.cluster
	if c == nil {
		return
	}
	d.seq++
	seq, gvr, path, all := d.seq, d.gvr, d.path, d.allValues
	d.loading, d.errMsg, d.dirty = true, "", false
	go func() {
		desc, derr := c.Describe(gvr, path)
		var det *backend.Details
		if !isHelm(gvr) {
			det, _ = c.Details(gvr, path)
		}
		var (
			y, vals string
			yerr    error
			sec     []backend.SecretEntry
		)
		if isHelm(gvr) {
			vals, yerr = c.HelmValues(path, all)
		} else {
			y, yerr = c.YAML(gvr, path)
		}
		if gvr == "v1/secrets" {
			sec, _ = c.SecretData(path)
		}
		d.app.post(func() {
			if seq != d.seq {
				return
			}
			d.loading = false
			if derr != nil {
				desc = derr.Error()
			}
			if yerr != nil {
				d.errMsg = yerr.Error()
			}
			d.describe.SetText(desc)
			d.details = det
			d.detV.reset()
			d.yaml.SetText(y)
			if strings.TrimSpace(vals) == "" || strings.TrimSpace(vals) == "{}" {
				vals = "# no user-supplied values"
			}
			d.values.SetText(vals)
			d.yamlV.set(y)
			d.valV.set(vals)
			d.original = y
			d.secret = sec
		})
	}()
}

func (d *Drawer) save() {
	c := d.app.cluster
	gvr, manifest := d.gvr, d.yaml.Text()
	d.app.do("Apply "+d.path, func() error {
		if err := c.Apply(gvr, manifest); err != nil {
			return err
		}
		d.app.post(func() {
			d.setEditing(false)
			d.load()
		})
		return nil
	})
}

func (d *Drawer) clickFor(m map[string]*widget.Clickable, k string) *widget.Clickable {
	c, ok := m[k]
	if !ok {
		c = new(widget.Clickable)
		m[k] = c
	}
	return c
}

// secretEnv renders the secret as KEY=value lines (binary values base64-free skipped).
func secretEnv(ee []backend.SecretEntry) string {
	var b strings.Builder
	for _, e := range ee {
		if e.Binary {
			continue
		}
		k := strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(e.Key))
		v := string(e.Value)
		if strings.ContainsAny(v, " \n\"'$#") {
			v = "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
		}
		b.WriteString(k + "=" + v + "\n")
	}
	return b.String()
}

func copyText(gtx C, s string) {
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(s))})
}

// Layout draws the drawer.
func (d *Drawer) Layout(gtx C) D {
	th := d.app.th
	for i := range d.tabBtns {
		if d.tabBtns[i].Clicked(gtx) {
			d.tab = i
		}
	}
	if d.closeBtn.Clicked(gtx) {
		d.close()
		return D{}
	}
	if d.editBtn.Clicked(gtx) && d.app.allowMutation("Edit") {
		d.setEditing(true)
	}
	if d.revertBtn.Clicked(gtx) {
		d.yaml.SetText(d.original)
		d.dirty = false
		d.setEditing(false)
	}
	if d.saveBtn.Clicked(gtx) && d.dirty && d.valMsg == "" && d.app.allowMutation("Apply") {
		d.app.overlay = newDiffReview(d.app, "Review changes", d.path, d.gvr, d.original, d.yaml.Text(), d.save)
	}
	if d.revealBtn.Clicked(gtx) {
		d.revealAll = !d.revealAll
		d.revealed = map[string]bool{}
	}
	if d.envBtn.Clicked(gtx) {
		copyText(gtx, secretEnv(d.secret))
		d.app.flash("Secret copied as .env ("+itoa(len(d.secret))+" keys)", false)
	}
	if d.dlBtn.Clicked(gtx) {
		d.downloadSecret()
	}
	if d.allValBtn.Clicked(gtx) {
		d.allValues = !d.allValues
		d.load()
	}
	for {
		ev, ok := d.yaml.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok && d.editing {
			d.dirty = d.yaml.Text() != d.original
			d.valMsg, d.valLine, d.valWarn = validateManifest(d.original, d.yaml.Text())
		}
	}
	fill(gtx, pal.Panel)
	var tabItems []layout.FlexChild
	for _, t := range d.tabs() {
		t := t
		tabItems = append(tabItems, layout.Rigid(func(gtx C) D { return d.tabButton(gtx, t) }))
	}
	tabItems = append(tabItems, layout.Flexed(1, layout.Spacer{}.Layout), layout.Rigid(d.layoutTabActions))

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return bg(gtx, pal.Header, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return insetHV(12, 8).Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D { return mdi(gtx, iconFor(d.gvr), pal.Accent, 18) }),
								layout.Rigid(layout.Spacer{Width: 8}.Layout),
								layout.Rigid(func(gtx C) D { return lbl(gtx, th, fontSans, 12, pal.TextDim, titleFor(d.gvr)) }),
								layout.Rigid(layout.Spacer{Width: 8}.Layout),
								layout.Flexed(1, func(gtx C) D { return lbl(gtx, th, fontSans, 14, pal.TextBright, d.path) }),
							)
						}),
						layout.Rigid(func(gtx C) D { return iconOnly(gtx, &d.closeBtn, icClose, 16) }),
					)
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			return insetHV(8, 4).Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, tabItems...)
			})
		}),
		layout.Rigid(func(gtx C) D { return hline(gtx, pal.Border) }),
		layout.Flexed(1, func(gtx C) D {
			if d.loading {
				return layout.Center.Layout(gtx, func(gtx C) D {
					return lbl(gtx, th, fontSans, 13, pal.TextDim, "Loading…")
				})
			}
			gtx.Constraints.Min = gtx.Constraints.Max
			if d.tab == tabData {
				return d.layoutSecret(gtx)
			}
			if d.tab == tabOverview {
				return d.detV.Layout(gtx, d.details)
			}
			if d.editing {
				fill(gtx, pal.Bg)
			}
			if !d.editing && (d.tab == tabYAML || d.tab == tabValues) && d.errMsg == "" {
				v := d.yamlV
				if d.tab == tabValues {
					v = d.valV
				}
				return inset(8).Layout(gtx, func(gtx C) D { return v.Layout(gtx, th) })
			}
			if d.editing {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return inset(10).Layout(gtx, func(gtx C) D {
							e := material.Editor(th, &d.yaml, "")
							e.Font, e.TextSize, e.Color = fontMono, 12, pal.Text
							return e.Layout(gtx)
						})
					}),
					layout.Rigid(d.layoutValidation),
				)
			}
			return inset(10).Layout(gtx, func(gtx C) D {
				ed := &d.describe
				switch d.tab {
				case tabYAML:
					ed = &d.yaml
				case tabValues:
					ed = &d.values
				}
				e := material.Editor(th, ed, "")
				e.Font = fontMono
				e.TextSize = 12
				e.Color = pal.Text
				if (d.tab == tabYAML || d.tab == tabValues) && d.errMsg != "" {
					e.Hint = d.errMsg
				}
				return e.Layout(gtx)
			})
		}),
	)
}

func (d *Drawer) layoutTabActions(gtx C) D {
	th := d.app.th
	switch d.tab {
	case tabYAML:
		if d.app.readOnly() {
			return iconText(gtx, th, icLock, 12, pal.TextDim, "read-only")
		}
		if !d.editing {
			return pillButton(gtx, th, &d.editBtn, icEdit, "Edit", pal.Text, colorNRGBA{})
		}
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &d.revertBtn, icClose, "Cancel", pal.Text, colorNRGBA{}) }),
			layout.Rigid(layout.Spacer{Width: 6}.Layout),
			layout.Rigid(func(gtx C) D {
				c := pal.Accent
				if !d.dirty || d.valMsg != "" {
					c = pal.Border
				}
				return pillButton(gtx, th, &d.saveBtn, icStatusDone, "Save & Apply", pal.TextBright, c)
			}),
		)
	case tabData:
		ic, txt := icEye, "Reveal"
		if d.revealAll {
			ic, txt = icEyeOff, "Hide"
		}
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &d.revealBtn, ic, txt, pal.Text, colorNRGBA{}) }),
			layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &d.envBtn, icCopyText, "Copy .env", pal.Text, colorNRGBA{}) }),
			layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &d.dlBtn, icDownload, "Download", pal.Text, colorNRGBA{}) }),
		)
	case tabValues:
		return toggle(gtx, th, &d.allValBtn, "Computed (all) values", d.allValues)
	}
	return D{}
}

func (d *Drawer) layoutSecret(gtx C) D {
	th := d.app.th
	if len(d.secret) == 0 {
		return layout.Center.Layout(gtx, func(gtx C) D {
			return lbl(gtx, th, fontSans, 13, pal.TextDim, "Secret has no data")
		})
	}
	return inset(10).Layout(gtx, func(gtx C) D {
		return material.List(th, &d.secretList).Layout(gtx, len(d.secret), func(gtx C, i int) D {
			e := d.secret[i]
			eye, cp := d.clickFor(d.keyEye, e.Key), d.clickFor(d.keyCopy, e.Key)
			if eye.Clicked(gtx) {
				d.revealed[e.Key] = !d.shown(e.Key)
			}
			if cp.Clicked(gtx) {
				copyText(gtx, string(e.Value))
				d.app.flash("Copied "+e.Key+" to clipboard", false)
			}
			shown := d.shown(e.Key)
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
				return rbg(gtx, pal.Bg, 8, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return inset(10).Layout(gtx, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx C) D { return mdi(gtx, icSecret, pal.Warn, 15) }),
									layout.Rigid(layout.Spacer{Width: 8}.Layout),
									layout.Flexed(1, func(gtx C) D { return lbl(gtx, th, fontMono, 13, pal.TextBright, e.Key) }),
									layout.Rigid(func(gtx C) D {
										return badge(gtx, th, itoa(len(e.Value))+" B", pal.TextDim, pal.Panel)
									}),
									layout.Rigid(layout.Spacer{Width: 6}.Layout),
									layout.Rigid(func(gtx C) D {
										ic := icEye
										if shown {
											ic = icEyeOff
										}
										return iconOnly(gtx, eye, ic, 15)
									}),
									layout.Rigid(func(gtx C) D { return iconOnly(gtx, cp, icCopyText, 15) }),
								)
							}),
							layout.Rigid(layout.Spacer{Height: 6}.Layout),
							layout.Rigid(func(gtx C) D {
								v := "••••••••••••"
								c := pal.TextDim
								switch {
								case e.Binary && shown:
									v = "<binary data, " + itoa(len(e.Value)) + " bytes>"
								case shown:
									v, c = string(e.Value), pal.Ok
								}
								l := material.Body2(th, v)
								l.Font = fontMono
								l.TextSize = 12
								l.Color = c
								l.MaxLines = 12
								return l.Layout(gtx)
							}),
						)
					})
				})
			})
		})
	})
}

func (d *Drawer) shown(k string) bool {
	if v, ok := d.revealed[k]; ok {
		return v
	}
	return d.revealAll
}

func (d *Drawer) downloadSecret() {
	dir := downloadsDir()
	name := strings.ReplaceAll(d.path, "/", "_") + ".env"
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(secretEnv(d.secret)), 0o600); err != nil {
		d.app.flash("download failed: "+err.Error(), true)
		return
	}
	d.app.flash("Saved "+p+" (mode 600)", false)
}

func (d *Drawer) tabButton(gtx C, i int) D {
	th := d.app.th
	active := d.tab == i
	cl := &d.tabBtns[i]
	ic := icDescribe
	switch i {
	case tabYAML:
		ic = icYAML
	case tabData:
		ic = icSecret
	case tabValues:
		ic = icValues
	case tabOverview:
		ic = icInfo
	}
	dims := cl.Layout(gtx, func(gtx C) D {
		c := pal.TextDim
		if active || cl.Hovered() {
			c = pal.TextBright
		}
		return insetHV(10, 7).Layout(gtx, func(gtx C) D { return iconText(gtx, th, ic, 13, c, tabNames[i]) })
	})
	if active {
		fillRRect(gtx, pal.Accent, rect(gtx.Dp(6), dims.Size.Y-gtx.Dp(2), dims.Size.X-gtx.Dp(6), dims.Size.Y), gtx.Dp(1))
	}
	return dims
}

// layoutValidation shows the live validation state while editing YAML.
func (d *Drawer) layoutValidation(gtx C) D {
	th := d.app.th
	ic, c, msg := icOK, pal.Ok, "Valid YAML"
	switch {
	case d.valMsg != "":
		ic, c, msg = icError, pal.Err, d.valMsg
		if d.valLine > 0 {
			msg = fmt.Sprintf("line %d: %s", d.valLine, d.valMsg)
		}
	case d.valWarn != "":
		ic, c, msg = icWarning, pal.Warn, d.valWarn
	case !d.dirty:
		ic, c, msg = icInfo, pal.TextDim, "No changes yet"
	}
	return bg(gtx, withAlpha(c, 0x1c), func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return insetHV(12, 6).Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D { return iconText(gtx, th, ic, 12, c, msg) }),
				layout.Rigid(func(gtx C) D {
					return lbl(gtx, th, fontSans, 11, pal.TextDim, "Save opens a diff + server dry run")
				}),
			)
		})
	})
}
