package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/richtext"

	"github.com/giovanni/kubeon/internal/backend"
)

const maxLogLines = 200_000

// Log levels detected from JSON fields, logfmt or plain text.
const (
	lvlUnknown uint8 = iota
	lvlDebug
	lvlInfo
	lvlWarn
	lvlError
)

var levelNames = map[uint8]string{lvlDebug: "DEBUG", lvlInfo: "INFO", lvlWarn: "WARN", lvlError: "ERROR"}

// logEntry is a log line plus parsed structure.
type logEntry struct {
	backend.LogLine
	level  uint8
	json   bool
	time   string
	msg    string
	fields string // remaining JSON fields as k=v
}

// LogView streams logs (k9s dao TailLogs) into a virtualized list, with
// multi-pod prefixes, JSON pretty-printing, level filtering and export.
type LogView struct {
	app       *App
	gvr, path string
	previous  bool
	cancel    context.CancelFunc

	lines    []logEntry
	multiCo  bool
	multiPod bool
	filtered []int
	filterOn bool
	rx       *regexp.Regexp
	rawQ     string
	minLevel uint8
	err      string

	list   widget.List
	filter widget.Editor
	rt     richtext.InteractiveText

	wrap, timestamps, allContainers, pretty bool

	followBtn, wrapBtn, tsBtn, clearBtn, copyBtn, allBtn, jsonBtn, lvlBtn, dlBtn widget.Clickable
}

func newLogView(a *App, gvr, path string, previous bool) *LogView {
	l := LogView{app: a, gvr: gvr, path: path, previous: previous, wrap: true, allContainers: true, pretty: true}
	l.list.Axis = layout.Vertical
	l.list.ScrollToEnd = true
	l.filter.SingleLine = true
	l.start()
	return &l
}

func (l *LogView) Title() string {
	t := "Logs " + shortName(l.path)
	if l.gvr != "v1/pods" {
		t = "Logs " + titleFor(l.gvr) + "/" + shortName(l.path)
	}
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
	l.lines, l.filtered, l.err = l.lines[:0], l.filtered[:0], ""
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

var (
	plainLevelRx  = regexp.MustCompile(`(?i)\b(?:level|lvl|severity)[=:]\s*"?(debug|info|warn|warning|error|fatal|panic|critical|trace)\b|\b(DEBUG|INFO|WARN|WARNING|ERROR|FATAL|PANIC|TRACE)\b`)
	jsonLevelKeys = []string{"level", "lvl", "severity", "log.level", "loglevel"}
	jsonMsgKeys   = []string{"msg", "message", "log", "event"}
	jsonTimeKeys  = []string{"time", "ts", "timestamp", "@timestamp", "t"}
)

func parseLevel(s string) uint8 {
	switch strings.ToLower(s) {
	case "debug", "trace", "dbg":
		return lvlDebug
	case "info", "information", "notice":
		return lvlInfo
	case "warn", "warning":
		return lvlWarn
	case "error", "err", "fatal", "panic", "critical", "crit", "emerg", "alert":
		return lvlError
	}
	return lvlUnknown
}

func parseEntry(ln backend.LogLine) logEntry {
	e := logEntry{LogLine: ln}
	t := strings.TrimSpace(ln.Text)
	if strings.HasPrefix(t, "{") && strings.HasSuffix(t, "}") {
		var m map[string]any
		if json.Unmarshal([]byte(t), &m) == nil {
			e.json = true
			take := func(keys []string) string {
				for _, k := range keys {
					if v, ok := m[k]; ok {
						delete(m, k)
						return fmt.Sprint(v)
					}
				}
				return ""
			}
			e.level = parseLevel(take(jsonLevelKeys))
			e.msg = take(jsonMsgKeys)
			e.time = take(jsonTimeKeys)
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var b strings.Builder
			for _, k := range keys {
				v, _ := json.Marshal(m[k])
				vs := string(v)
				if s, ok := m[k].(string); ok && !strings.ContainsAny(s, " \t\"") {
					vs = s
				}
				fmt.Fprintf(&b, " %s=%s", k, vs)
			}
			e.fields = b.String()
			return e
		}
	}
	if mm := plainLevelRx.FindStringSubmatch(t); mm != nil {
		e.level = parseLevel(mm[1] + mm[2])
	}
	if ln.IsError {
		e.level = lvlError
	}
	return e
}

func (l *LogView) match(e *logEntry) bool {
	if l.minLevel > 0 && e.level < l.minLevel {
		return false
	}
	return l.rx == nil || l.rx.MatchString(e.Text)
}

func (l *LogView) append(batch []backend.LogLine) {
	base := len(l.lines)
	for _, ln := range batch {
		l.lines = append(l.lines, parseEntry(ln))
		if ln.Pod != "" && len(l.lines) > 0 && ln.Pod != l.lines[0].Pod {
			l.multiPod = true
		}
		if ln.Container != "" && ln.Container != l.lines[0].Container {
			l.multiCo = true
		}
	}
	if l.filterOn {
		for i := base; i < len(l.lines); i++ {
			if l.match(&l.lines[i]) {
				l.filtered = append(l.filtered, i)
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
	l.filterOn = l.rx != nil || l.minLevel > 0
	if !l.filterOn {
		return
	}
	for i := range l.lines {
		if l.match(&l.lines[i]) {
			l.filtered = append(l.filtered, i)
		}
	}
}

func (l *LogView) count() int {
	if l.filterOn {
		return len(l.filtered)
	}
	return len(l.lines)
}

func (l *LogView) at(i int) *logEntry {
	if l.filterOn {
		return &l.lines[l.filtered[i]]
	}
	return &l.lines[i]
}

func (l *LogView) following() bool { return !l.list.Position.BeforeEnd }

// export renders the visible (filtered) lines as plain text.
func (l *LogView) export() string {
	var b strings.Builder
	for i := 0; i < l.count(); i++ {
		e := l.at(i)
		if l.multiPod && e.Pod != "" {
			b.WriteString("[" + e.Pod + "] ")
		}
		if l.multiCo && e.Container != "" {
			b.WriteString("[" + e.Container + "] ")
		}
		b.WriteString(e.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func (l *LogView) download() {
	name := fmt.Sprintf("%s-%s.log", strings.ReplaceAll(l.path, "/", "_"), time.Now().Format("20060102-150405"))
	p, err := saveNew(name, []byte(l.export()))
	if err != nil {
		l.app.flash("download failed: "+err.Error(), true)
		return
	}
	l.app.flash(fmt.Sprintf("Saved %d lines to %s", l.count(), p), false)
}

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
	if l.jsonBtn.Clicked(gtx) {
		l.pretty = !l.pretty
	}
	if l.lvlBtn.Clicked(gtx) {
		switch l.minLevel {
		case lvlUnknown:
			l.minLevel = lvlWarn
		case lvlWarn:
			l.minLevel = lvlError
		default:
			l.minLevel = lvlUnknown
		}
		l.rebuildFilter()
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
		gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(l.export()))})
		l.app.flash("Logs copied to clipboard", false)
	}
	if l.dlBtn.Clicked(gtx) {
		l.download()
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return insetHV(8, 4).Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Max.X = gtx.Dp(240)
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
					layout.Rigid(layout.Spacer{Width: 6}.Layout),
					layout.Rigid(func(gtx C) D { return l.levelButton(gtx) }),
					layout.Rigid(func(gtx C) D { return toggle(gtx, th, &l.followBtn, "Follow", l.following()) }),
					layout.Rigid(func(gtx C) D { return toggle(gtx, th, &l.wrapBtn, "Wrap", l.wrap) }),
					layout.Rigid(func(gtx C) D { return toggle(gtx, th, &l.jsonBtn, "JSON", l.pretty) }),
					layout.Rigid(func(gtx C) D { return toggle(gtx, th, &l.tsBtn, "Time", l.timestamps) }),
					layout.Rigid(func(gtx C) D { return toggle(gtx, th, &l.allBtn, "All containers", l.allContainers) }),
					layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &l.clearBtn, icClear, "", pal.TextDim, colorNRGBA{}) }),
					layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &l.copyBtn, icCopyText, "", pal.TextDim, colorNRGBA{}) }),
					layout.Rigid(func(gtx C) D { return pillButton(gtx, th, &l.dlBtn, icDownload, "", pal.TextDim, colorNRGBA{}) }),
					layout.Flexed(1, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return layout.E.Layout(gtx, func(gtx C) D {
							msg := itoa(l.count()) + " lines"
							if l.filterOn {
								msg += " / " + itoa(len(l.lines))
							}
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
			fill(gtx, pal.CodeBg)
			return layout.Inset{Left: 8, Right: 4}.Layout(gtx, func(gtx C) D {
				return material.List(th, &l.list).Layout(gtx, l.count(), func(gtx C, i int) D {
					return l.layoutLine(gtx, l.at(i))
				})
			})
		}),
	)
}

func (l *LogView) levelButton(gtx C) D {
	th := l.app.th
	txt, c := "All levels", pal.TextDim
	switch l.minLevel {
	case lvlWarn:
		txt, c = "Warn+", pal.Warn
	case lvlError:
		txt, c = "Errors", pal.Err
	}
	return pillButton(gtx, th, &l.lvlBtn, icFilter, txt, c, colorNRGBA{})
}

func levelColor(lv uint8) colorNRGBA {
	switch lv {
	case lvlDebug:
		return pal.TextDim
	case lvlInfo:
		return pal.Info
	case lvlWarn:
		return pal.Warn
	case lvlError:
		return pal.Err
	}
	return pal.Text
}

func (l *LogView) layoutLine(gtx C, e *logEntry) D {
	th := l.app.th
	const size = unit.Sp(12)
	var spans []richtext.SpanStyle
	add := func(s string, c colorNRGBA) {
		if s != "" {
			spans = append(spans, richtext.SpanStyle{Font: fontMono, Size: size, Color: c, Content: s})
		}
	}
	if l.multiPod && e.Pod != "" {
		add(podTag(e.Pod)+" ", containerColor(e.Pod))
	}
	if l.multiCo && e.Container != "" {
		add(e.Container+" ", containerColor(e.Container))
	}
	textC := pal.Text
	if e.level == lvlError {
		textC = lighten(pal.Err, 0.25)
	}
	if l.pretty && e.json {
		if e.time != "" && l.timestamps {
			add(e.time+" ", pal.TextDim)
		}
		if e.level != lvlUnknown {
			add(fmt.Sprintf("%-5s ", levelNames[e.level]), levelColor(e.level))
		}
		add(e.msg, pal.TextBright)
		add(e.fields, pal.TextDim)
	} else {
		if e.level >= lvlWarn {
			textC = levelColor(e.level)
			if e.level == lvlError {
				textC = lighten(pal.Err, 0.25)
			}
		}
		add(e.Text, textC)
	}
	if len(spans) == 0 {
		add(" ", pal.Text)
	}
	t := richtext.Text(&l.rt, th.Shaper, spans...)
	if l.wrap {
		return t.Layout(gtx)
	}
	// single line: lay out unconstrained, clip to the visible width
	w := gtx.Constraints.Max.X
	g := gtx
	g.Constraints.Max.X = 1 << 20
	g.Constraints.Min.X = 0
	defer clip.Rect{Max: image.Pt(w, gtx.Constraints.Max.Y)}.Push(gtx.Ops).Pop()
	d := t.Layout(g)
	lh := gtx.Sp(size) * 14 / 10
	if d.Size.Y > lh {
		d.Size.Y = lh
	}
	d.Size.X = w
	return d
}

// podTag shortens a pod name to its unique suffix (rs hash + pod id).
func podTag(pod string) string {
	parts := strings.Split(pod, "-")
	if len(parts) >= 3 {
		return parts[len(parts)-1]
	}
	return pod
}

var containerPalette = []uint32{0x5fb3e8, 0xc792ea, 0x89ddff, 0xffcb6b, 0xc3e88d, 0xf78c6c}
var containerPaletteLight = []uint32{0x1f6fb2, 0x8a3fc4, 0x0b7a8a, 0xa86a00, 0x3d7a1c, 0xc24a1f}

func containerColor(name string) colorNRGBA {
	h := 0
	for _, r := range name {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	if pal.Light {
		return rgb(containerPaletteLight[h%len(containerPaletteLight)])
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
	return fmt.Sprint(n)
}
