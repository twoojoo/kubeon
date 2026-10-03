package ui

import (
	"gioui.org/io/event"
	"gioui.org/io/key"
)

// handleKeys implements k9s-style global shortcuts. Letter shortcuts are
// only active while no text input (filter, editor, terminal) has focus.
func (a *App) handleKeys(gtx C) {
	filters := []event.Filter{
		key.Filter{Name: key.NameEscape},
		key.Filter{Name: "K", Required: key.ModShortcut},
		key.Filter{Name: "P", Required: key.ModShortcut, Optional: key.ModShift},
	}
	nav := a.cluster != nil && !a.textFocused(gtx)
	if nav {
		filters = append(filters,
			key.Filter{Name: ":", Optional: key.ModShift},
			key.Filter{Name: ";", Required: key.ModShift},
			key.Filter{Name: "/"},
			key.Filter{Name: key.NameUpArrow}, key.Filter{Name: key.NameDownArrow},
			key.Filter{Name: key.NamePageUp}, key.Filter{Name: key.NamePageDown},
			key.Filter{Name: key.NameHome}, key.Filter{Name: key.NameEnd},
			key.Filter{Name: key.NameReturn}, key.Filter{Name: key.NameEnter},
			key.Filter{Name: "J"}, key.Filter{Name: "K"},
			key.Filter{Name: "G", Optional: key.ModShift},
			key.Filter{Name: "L"}, key.Filter{Name: "P"}, key.Filter{Name: "S"},
			key.Filter{Name: "Y"}, key.Filter{Name: "D"}, key.Filter{Name: "E"},
			key.Filter{Name: "W"}, key.Filter{Name: "N"}, key.Filter{Name: "R"},
			key.Filter{Name: "D", Required: key.ModCtrl},
			key.Filter{Name: "K", Required: key.ModCtrl},
			key.Filter{Name: "R", Required: key.ModCtrl},
			key.Filter{Name: "0"}, key.Filter{Name: "1"}, key.Filter{Name: "2"},
		)
	}
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		a.onKey(gtx, e, nav)
	}
}

func (a *App) onKey(gtx C, e key.Event, nav bool) {
	v := a.view
	ctrl := e.Modifiers.Contain(key.ModCtrl)
	switch {
	case e.Name == key.NameEscape:
		a.escape(gtx)
	case e.Modifiers.Contain(key.ModShortcut) && e.Name == "K",
		e.Modifiers.Contain(key.ModShortcut) && e.Name == "P":
		a.openPalette("")
	case !nav:
		return
	case e.Name == ":" || e.Name == ";":
		a.openPalette("")
	case e.Name == "/":
		gtx.Execute(key.FocusCmd{Tag: &v.filter})
	case e.Name == key.NameUpArrow || e.Name == "K" && !ctrl:
		v.move(-1)
	case e.Name == key.NameDownArrow || e.Name == "J":
		v.move(1)
	case e.Name == key.NamePageUp:
		v.move(-v.pageSize())
	case e.Name == key.NamePageDown:
		v.move(v.pageSize())
	case e.Name == key.NameHome || e.Name == "G" && !e.Modifiers.Contain(key.ModShift):
		v.move(-1 << 30)
	case e.Name == key.NameEnd || e.Name == "G":
		v.move(1 << 30)
	case e.Name == key.NameReturn || e.Name == key.NameEnter:
		v.enter()
	case e.Name == "D" && ctrl:
		v.confirmDelete()
	case e.Name == "K" && ctrl:
		v.confirmDelete()
	case e.Name == "R" && ctrl:
		v.refresh()
	case e.Name == "L":
		v.logs(false)
	case e.Name == "P":
		v.logs(true)
	case e.Name == "S":
		v.shell()
	case e.Name == "Y":
		v.details(tabYAML)
	case e.Name == "D":
		v.details(tabDescribe)
	case e.Name == "E":
		v.edit()
	case e.Name == "W":
		v.toggleWide()
	case e.Name == "N":
		a.pickNamespace()
	case e.Name == "R":
		v.restart()
	case e.Name == "0":
		a.setNamespace("")
	}
}

func (a *App) escape(gtx C) {
	switch {
	case a.overlay != nil:
		a.overlay = nil
		gtx.Execute(key.FocusCmd{})
	case a.textFocused(gtx):
		gtx.Execute(key.FocusCmd{})
	case a.drawer.open:
		a.drawer.close()
	case a.view.filter.Len() > 0:
		a.view.filter.SetText("")
		a.view.applyFilter()
	}
}
