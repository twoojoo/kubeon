package ui

import (
	"testing"

	"gioui.org/io/key"
	"github.com/hinshun/vt10x"
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
