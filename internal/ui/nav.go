package ui

// navState identifies what the main area shows.
type navState struct {
	page  string // overviewGVR, xrayPage or a gvr (resource table)
	gvr   string
	path  string // owner scope (helm history) or xray focus
	selID string
}

const xrayPage = "xray"

func (a *App) current() navState {
	st := navState{page: a.page}
	switch a.page {
	case xrayPage:
		st.gvr, st.path = a.xray.gvr, a.xray.focus
	case overviewGVR, sanitizerPage, rbacPage:
	default:
		st.gvr, st.path, st.selID = a.view.gvr, a.view.path, a.view.selID
	}
	return st
}

// show opens a page without touching the history stack.
func (a *App) show(st navState) {
	if a.cluster == nil {
		return
	}
	a.drawer.close()
	a.page = st.page
	switch st.page {
	case overviewGVR:
		a.view.close()
		a.view.gvr = ""
		a.xray.close()
		a.ovw.open(a.ns)
		a.sidebar.active = overviewGVR
	case xrayPage:
		a.view.close()
		a.view.gvr = ""
		a.ovw.close()
		a.xray.open(st.gvr, a.ns, st.path)
	case rbacPage:
		a.view.close()
		a.view.gvr = ""
		a.ovw.close()
		a.xray.close()
		a.rbac.open(a.ns)
		a.sidebar.active = rbacPage
	case sanitizerPage:
		a.view.close()
		a.view.gvr = ""
		a.ovw.close()
		a.xray.close()
		a.san.open(a.ns)
		a.sidebar.active = sanitizerPage
	default:
		a.ovw.close()
		a.xray.close()
		a.view.openPath(st.gvr, a.ns, st.path)
		a.view.selID = st.selID
		if st.path == "" {
			a.sidebar.active = st.gvr
		}
	}
}

// drill opens st and remembers where we came from (Esc / back goes back).
func (a *App) drill(st navState) {
	a.navStack = append(a.navStack, a.current())
	if len(a.navStack) > 32 {
		a.navStack = a.navStack[1:]
	}
	a.show(st)
}

// back returns to the previous page; false when there is none.
func (a *App) back() bool {
	n := len(a.navStack)
	if n == 0 {
		return false
	}
	st := a.navStack[n-1]
	a.navStack = a.navStack[:n-1]
	a.show(st)
	return true
}
