package ui

import (
	"math"
	"testing"

	"gioui.org/io/key"
	"github.com/hinshun/vt10x"

	"github.com/giovanni/kubeon/internal/prefs"
)

func TestKeySeq(t *testing.T) {
	cases := []struct {
		e    key.Event
		app  bool
		want string
	}{
		{key.Event{Name: key.NameReturn}, false, "\r"},
		{key.Event{Name: key.NameUpArrow}, false, "\x1b[A"},
		{key.Event{Name: key.NameUpArrow}, true, "\x1bOA"},
		{key.Event{Name: "C", Modifiers: key.ModCtrl}, false, "\x03"},
		{key.Event{Name: key.NameTab, Modifiers: key.ModShift}, false, "\x1b[Z"},
	}
	for _, c := range cases {
		if got := keySeq(c.e, c.app); got != c.want {
			t.Errorf("%v: got %q want %q", c.e.Name, got, c.want)
		}
	}
}

func TestVTColor(t *testing.T) {
	def := rgb(0x123456)
	if vtColor(vt10x.DefaultFG, def) != def {
		t.Fatal("default fg")
	}
	if vtColor(vt10x.Red, def) != rgb(ansi16[1]) {
		t.Fatal("ansi red")
	}
	if vtColor(16, def) != rgb(0x000000) || vtColor(231, def) != rgb(0xffffff) {
		t.Fatal("xterm cube bounds")
	}
}

func TestPickerFilterPrefersPrefix(t *testing.T) {
	p := newPicker(&App{}, "t", []string{"Endpoint Slices", "Deployments", "DaemonSets"}, false, nil)
	p.editor.SetText("de")
	p.refilter()
	if len(p.matches) == 0 || p.items[p.matches[0]] != "Deployments" {
		t.Fatalf("got %v", p.matches)
	}
}

func TestUIScaleClamp(t *testing.T) {
	t.Setenv("KUBEON_CONFIG_DIR", t.TempDir())
	a := &App{prefs: prefs.Load()}
	for in, want := range map[float64]float64{0: 1, -2: 1, math.NaN(): 1, math.Inf(1): 1, 100: 1.8, 0.01: 0.8, 1.25: 1.25} {
		a.prefs.UIScale = in
		if got := a.uiScale(); got != want {
			t.Errorf("uiScale(%v) = %v, want %v", in, got, want)
		}
	}
	a.prefs.UIScale = 1.8
	a.zoom(1)
	if a.uiScale() != 1.8 {
		t.Error("zoom past the largest step")
	}
	a.zoom(-1)
	if a.uiScale() != 1.6 {
		t.Errorf("zoom out %v", a.uiScale())
	}
	a.zoom(0)
	if a.uiScale() != 1 {
		t.Error("zoom reset")
	}
	if got := prefs.Load().UIScale; got != 1 {
		t.Errorf("saved scale %v", got)
	}
}
