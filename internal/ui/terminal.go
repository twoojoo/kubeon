package ui

import (
	"image"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"github.com/hinshun/vt10x"

	"github.com/giovanni/k9desk/internal/backend"
)

const termText = unit.Sp(13)

// Terminal is a pod shell: k8s exec stream rendered by a vt10x emulator.
type Terminal struct {
	app             *App
	path, container string

	vt   vt10x.Terminal
	sess *backend.ExecSession
	in   *lazyWriter

	mx     sync.Mutex
	exited string

	tag          int
	cols, rows   int
	cellW, cellH int
	started      bool
}

// lazyWriter lets vt10x answer terminal queries before the session exists.
type lazyWriter struct {
	mx sync.Mutex
	w  io.Writer
}

func (l *lazyWriter) Write(p []byte) (int, error) {
	l.mx.Lock()
	defer l.mx.Unlock()
	if l.w == nil {
		return len(p), nil
	}
	return l.w.Write(p)
}

func newTerminal(a *App, path, container string) *Terminal {
	t := Terminal{app: a, path: path, container: container, in: &lazyWriter{}}
	t.vt = vt10x.New(vt10x.WithWriter(t.in), vt10x.WithSize(80, 24))
	return &t
}

func (t *Terminal) Title() string {
	return "Shell " + shortName(t.path) + "/" + t.container
}

func (t *Terminal) Focused(gtx C) bool { return gtx.Focused(&t.tag) }

func (t *Terminal) Close() {
	if t.sess != nil {
		t.sess.Close()
	}
}

// Write receives exec output.
func (t *Terminal) Write(p []byte) (int, error) {
	n, err := t.vt.Write(p)
	t.app.invalidate()
	return n, err
}

func (t *Terminal) start(cols, rows int) {
	t.started = true
	sess, err := t.app.cluster.Exec(t.path, t.container, nil,
		backend.TermSize{Cols: uint16(cols), Rows: uint16(rows)}, t,
		func(err error) {
			msg := "[session closed]"
			if err != nil {
				msg = "[session closed: " + err.Error() + "]"
			}
			t.mx.Lock()
			t.exited = msg
			t.mx.Unlock()
			t.app.invalidate()
		})
	if err != nil {
		t.exited = "exec failed: " + err.Error()
		return
	}
	t.sess = sess
	t.in.mx.Lock()
	t.in.w = sess.Stdin
	t.in.mx.Unlock()
}

func (t *Terminal) send(s string) {
	if t.sess != nil && s != "" {
		_, _ = io.WriteString(t.sess.Stdin, s)
	}
}

var termKeys = []key.Name{
	key.NameReturn, key.NameEnter, key.NameDeleteBackward, key.NameDeleteForward,
	key.NameEscape, key.NameTab, key.NameUpArrow, key.NameDownArrow,
	key.NameLeftArrow, key.NameRightArrow, key.NameHome, key.NameEnd,
	key.NamePageUp, key.NamePageDown,
}

func (t *Terminal) handleInput(gtx C) {
	filters := []event.Filter{
		key.FocusFilter{Target: &t.tag},
		transfer.TargetFilter{Target: &t.tag, Type: "application/text"},
		pointer.Filter{Target: &t.tag, Kinds: pointer.Press},
		key.Filter{Focus: &t.tag, Name: "V", Required: key.ModCtrl | key.ModShift},
	}
	for _, n := range termKeys {
		filters = append(filters, key.Filter{Focus: &t.tag, Name: n, Optional: key.ModShift})
	}
	for c := 'A'; c <= 'Z'; c++ {
		filters = append(filters, key.Filter{Focus: &t.tag, Name: key.Name(string(c)), Required: key.ModCtrl})
	}
	appCursor := t.vt.Mode()&vt10x.ModeAppCursor != 0
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		switch e := ev.(type) {
		case pointer.Event:
			gtx.Execute(key.FocusCmd{Tag: &t.tag})
		case key.EditEvent:
			t.send(e.Text)
		case transfer.DataEvent:
			if r := e.Open(); r != nil {
				b, _ := io.ReadAll(r)
				_ = r.Close()
				t.send(string(b))
			}
		case key.Event:
			if e.State != key.Press {
				continue
			}
			if e.Name == "V" && e.Modifiers.Contain(key.ModShift) {
				gtx.Execute(clipboard.ReadCmd{Tag: &t.tag})
				continue
			}
			t.send(keySeq(e, appCursor))
		}
	}
}

func keySeq(e key.Event, appCursor bool) string {
	if e.Modifiers.Contain(key.ModCtrl) && len(e.Name) == 1 {
		c := e.Name[0]
		if c >= 'A' && c <= 'Z' {
			return string(rune(c - 'A' + 1))
		}
	}
	arrow := func(c string) string {
		if appCursor {
			return "\x1bO" + c
		}
		return "\x1b[" + c
	}
	switch e.Name {
	case key.NameReturn, key.NameEnter:
		return "\r"
	case key.NameDeleteBackward:
		return "\x7f"
	case key.NameDeleteForward:
		return "\x1b[3~"
	case key.NameEscape:
		return "\x1b"
	case key.NameTab:
		if e.Modifiers.Contain(key.ModShift) {
			return "\x1b[Z"
		}
		return "\t"
	case key.NameUpArrow:
		return arrow("A")
	case key.NameDownArrow:
		return arrow("B")
	case key.NameRightArrow:
		return arrow("C")
	case key.NameLeftArrow:
		return arrow("D")
	case key.NameHome:
		return "\x1b[H"
	case key.NameEnd:
		return "\x1b[F"
	case key.NamePageUp:
		return "\x1b[5~"
	case key.NamePageDown:
		return "\x1b[6~"
	}
	return ""
}

func (t *Terminal) measure(gtx C) {
	if t.cellW > 0 {
		return
	}
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	g.Constraints.Max.X = 1 << 20
	d := lbl(g, t.app.th, fontMono, termText, pal.Text, strings.Repeat("M", 100))
	m.Stop()
	t.cellW = max(1, d.Size.X/100)
	t.cellH = max(1, d.Size.Y+gtx.Dp(1))
}

func (t *Terminal) Layout(gtx C) D {
	gtx.Constraints.Min = gtx.Constraints.Max
	termBg := rgb(0x101214)
	fill(gtx, termBg)
	t.measure(gtx)

	size := gtx.Constraints.Max
	padX, padY := gtx.Dp(8), gtx.Dp(6)
	cols := max(10, (size.X-2*padX)/t.cellW)
	rows := max(4, (size.Y-2*padY)/t.cellH)
	if cols != t.cols || rows != t.rows {
		t.cols, t.rows = cols, rows
		t.vt.Resize(cols, rows)
		if t.sess != nil {
			t.sess.Resize(backend.TermSize{Cols: uint16(cols), Rows: uint16(rows)})
		}
	}
	if !t.started {
		t.start(cols, rows)
		gtx.Execute(key.FocusCmd{Tag: &t.tag})
	}

	area := clip.Rect{Max: size}.Push(gtx.Ops)
	event.Op(gtx.Ops, &t.tag)
	pointer.CursorText.Add(gtx.Ops)
	t.handleInput(gtx)
	area.Pop()

	focused := gtx.Focused(&t.tag)
	th := t.app.th
	t.vt.Lock()
	cur := t.vt.Cursor()
	curVis := t.vt.CursorVisible()
	var run strings.Builder
	for y := 0; y < rows; y++ {
		x := 0
		for x < cols {
			g := t.vt.Cell(x, y)
			fg, bgc := g.FG, g.BG
			start := x
			run.Reset()
			for x < cols {
				c := t.vt.Cell(x, y)
				if c.FG != fg || c.BG != bgc {
					break
				}
				ch := c.Char
				if ch == 0 || !utf8.ValidRune(ch) {
					ch = ' '
				}
				run.WriteRune(ch)
				x++
			}
			px := padX + start*t.cellW
			py := padY + y*t.cellH
			if bgc != vt10x.DefaultBG {
				fillRect(gtx, vtColor(bgc, termBg), image.Rect(px, py, px+(x-start)*t.cellW, py+t.cellH))
			}
			s := strings.TrimRight(run.String(), " ")
			if s == "" {
				continue
			}
			st := op.Offset(image.Pt(px, py)).Push(gtx.Ops)
			g2 := gtx
			g2.Constraints = layout.Exact(image.Pt((x-start)*t.cellW+t.cellW, t.cellH))
			lbl(g2, th, fontMono, termText, vtColor(fg, pal.Text), s)
			st.Pop()
		}
	}
	t.vt.Unlock()

	if curVis && cur.X < cols && cur.Y < rows {
		px, py := padX+cur.X*t.cellW, padY+cur.Y*t.cellH
		c := withAlpha(pal.Accent, 0xa0)
		if !focused {
			c = withAlpha(pal.TextDim, 0x60)
		}
		fillRect(gtx, c, image.Rect(px, py, px+t.cellW, py+t.cellH))
	}

	t.mx.Lock()
	exited := t.exited
	t.mx.Unlock()
	if exited != "" {
		st := op.Offset(image.Pt(padX, size.Y-t.cellH-padY)).Push(gtx.Ops)
		lbl(gtx, th, fontMono, termText, pal.Warn, exited)
		st.Pop()
	}
	return D{Size: size}
}

var ansi16 = []uint32{
	0x1d1f21, 0xcc6666, 0xb5bd68, 0xf0c674, 0x81a2be, 0xb294bb, 0x8abeb7, 0xc5c8c6,
	0x666666, 0xd54e53, 0xb9ca4a, 0xe7c547, 0x7aa6da, 0xc397d8, 0x70c0b1, 0xeaeaea,
}

func vtColor(c vt10x.Color, def colorNRGBA) colorNRGBA {
	switch {
	case c == vt10x.DefaultFG || c == vt10x.DefaultBG || c == vt10x.DefaultCursor:
		return def
	case c < 16:
		return rgb(ansi16[c])
	case c < 232:
		i := uint32(c) - 16
		lv := func(v uint32) uint32 {
			if v == 0 {
				return 0
			}
			return 55 + v*40
		}
		return rgb(lv(i/36)<<16 | lv(i/6%6)<<8 | lv(i%6))
	case c < 256:
		v := 8 + (uint32(c)-232)*10
		return rgb(v<<16 | v<<8 | v)
	case c < 1<<24:
		return rgb(uint32(c))
	}
	return def
}
