// Package prefs persists user preferences in ~/.config/kubeon/settings.json:
// theme, per-context safety policies, saved port-forwards and bookmarks.
package prefs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// ContextPolicy is the safety policy of a kube context.
type ContextPolicy struct {
	// Protected marks a production-like context: red frame, type-to-confirm
	// for destructive actions. nil = auto-detect from the context name.
	Protected *bool `json:"protected,omitempty"`
	// ReadOnly blocks every mutating action (UI and assistant proposals).
	ReadOnly bool `json:"readOnly,omitempty"`
	// Namespace is the last namespace opened on the context ("" = all).
	Namespace string `json:"namespace,omitempty"`
}

// PortForward is a saved tunnel. Pinned ones (AutoStart) are re-opened when
// the context connects; a named tunnel that isn't pinned is only remembered.
type PortForward struct {
	Context   string `json:"context"`
	GVR       string `json:"gvr"`
	Path      string `json:"path"`
	Local     int    `json:"local"`
	Remote    int    `json:"remote"`
	AutoStart bool   `json:"autoStart"`
	Name      string `json:"name,omitempty"` // user label
}

// MaxForwardName is the longest tunnel name kept.
const MaxForwardName = 60

// CleanForwardName trims a user-typed tunnel name: surrounding space gone,
// inner whitespace (newlines, tabs) collapsed, at most MaxForwardName runes.
func CleanForwardName(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > MaxForwardName {
		s = strings.TrimSpace(string(r[:MaxForwardName]))
	}
	return s
}

// Bookmark is a saved view.
type Bookmark struct {
	Name      string `json:"name"`
	Context   string `json:"context"` // "" = any context
	GVR       string `json:"gvr"`
	Namespace string `json:"namespace"`
	Filter    string `json:"filter,omitempty"`
}

// Prefs is the persisted state.
type Prefs struct {
	Theme        string                   `json:"theme,omitempty"` // "dark" | "light"
	Contexts     map[string]ContextPolicy `json:"contexts,omitempty"`
	PortForwards []PortForward            `json:"portForwards,omitempty"`
	Bookmarks    []Bookmark               `json:"bookmarks,omitempty"`
	CheckUpdates *bool                    `json:"checkUpdates,omitempty"` // nil = on

	// LoadError reports a settings file that could not be parsed.
	LoadError error `json:"-"`

	mx   sync.Mutex
	path string
}

// Dir returns the config directory ($KUBEON_CONFIG_DIR overrides).
func Dir() string {
	if d := os.Getenv("KUBEON_CONFIG_DIR"); d != "" {
		return d
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "kubeon")
}

// Load reads the preferences (missing file → defaults).
func Load() *Prefs {
	p := Prefs{path: filepath.Join(Dir(), "settings.json")}
	if b, err := os.ReadFile(p.path); err == nil {
		if err := json.Unmarshal(b, &p); err != nil {
			// keep the broken file (context policies!) instead of
			// overwriting it with defaults on the next save
			p = Prefs{path: p.path, LoadError: fmt.Errorf("%s is invalid (%v); moved to %s", p.path, err, Quarantine(p.path, err))}
		}
	}
	if p.Contexts == nil {
		p.Contexts = map[string]ContextPolicy{}
	}
	if p.Theme != "light" {
		p.Theme = "dark"
	}
	return &p
}

// Save writes the preferences (0600).
func (p *Prefs) Save() error {
	p.mx.Lock()
	defer p.mx.Unlock()
	if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	return WriteFileAtomic(p.path, b)
}

var prodRx = regexp.MustCompile(`(?i)(^|[^a-z])(prod|production|prd|live)([^a-z]|$)`)

// LooksProduction guesses production contexts from their name.
func LooksProduction(ctx string) bool { return prodRx.MatchString(ctx) }

// Protected reports whether ctx is protected (explicit or auto-detected).
func (p *Prefs) Protected(ctx string) bool {
	if c, ok := p.Contexts[ctx]; ok && c.Protected != nil {
		return *c.Protected
	}
	return LooksProduction(ctx)
}

// ReadOnly reports whether ctx is read-only.
func (p *Prefs) ReadOnly(ctx string) bool { return p.Contexts[ctx].ReadOnly }

// UpdatesEnabled reports whether kubeon looks for new releases (default on).
func (p *Prefs) UpdatesEnabled() bool { return p.CheckUpdates == nil || *p.CheckUpdates }

// SetUpdates turns release checks on or off.
func (p *Prefs) SetUpdates(on bool) { p.CheckUpdates = &on }

// SetPolicy stores the policy of ctx.
func (p *Prefs) SetPolicy(ctx string, protected, readOnly bool) {
	c := p.Contexts[ctx]
	c.Protected, c.ReadOnly = &protected, readOnly
	p.Contexts[ctx] = c
}

// LastNamespace is the namespace last opened on ctx ("" = all).
func (p *Prefs) LastNamespace(ctx string) string { return p.Contexts[ctx].Namespace }

// SetLastNamespace remembers the namespace opened on ctx; it reports whether
// the value changed (i.e. whether the preferences need saving).
func (p *Prefs) SetLastNamespace(ctx, ns string) bool {
	c, ok := p.Contexts[ctx]
	if c.Namespace == ns && (ok || ns == "") {
		return false
	}
	c.Namespace = ns
	p.Contexts[ctx] = c
	return true
}

func samePF(a, b PortForward) bool {
	return a.Context == b.Context && a.Path == b.Path && a.Remote == b.Remote && a.GVR == b.GVR
}

// SavePortForward adds or updates a saved forward.
func (p *Prefs) SavePortForward(f PortForward) {
	for i := range p.PortForwards {
		if samePF(p.PortForwards[i], f) {
			p.PortForwards[i] = f
			return
		}
	}
	p.PortForwards = append(p.PortForwards, f)
}

// FindPortForward returns the saved entry for f (identified by context,
// resource and remote port), if any.
func (p *Prefs) FindPortForward(f PortForward) (PortForward, bool) {
	for _, x := range p.PortForwards {
		if samePF(x, f) {
			return x, true
		}
	}
	return PortForward{}, false
}

// SetPortForwardName names the tunnel f. An unsaved tunnel gets remembered
// (not pinned); clearing the name of one that isn't pinned forgets it.
func (p *Prefs) SetPortForwardName(f PortForward, name string) {
	name = CleanForwardName(name)
	cur, ok := p.FindPortForward(f)
	switch {
	case ok:
		cur.Name = name
		if name == "" && !cur.AutoStart {
			p.RemovePortForward(cur)
			return
		}
		p.SavePortForward(cur)
	case name != "":
		f.Name, f.AutoStart = name, false
		p.SavePortForward(f)
	}
}

// TogglePinPortForward pins f (re-opened on connect) or unpins it. Unpinning
// keeps a named tunnel and forgets an unnamed one. Returns the new state.
func (p *Prefs) TogglePinPortForward(f PortForward) bool {
	cur, ok := p.FindPortForward(f)
	switch {
	case !ok:
		f.AutoStart = true
		p.SavePortForward(f)
		return true
	case cur.AutoStart && cur.Name == "":
		p.RemovePortForward(cur)
		return false
	default:
		cur.AutoStart = !cur.AutoStart
		p.SavePortForward(cur)
		return cur.AutoStart
	}
}

// ForwardLabel is the user's name of tunnel f, or "".
func (p *Prefs) ForwardLabel(f PortForward) string {
	cur, _ := p.FindPortForward(f)
	return cur.Name
}

// PinnedForwards counts the tunnels of ctx re-opened on connect.
func (p *Prefs) PinnedForwards(ctx string) int {
	n := 0
	for _, f := range p.SavedForwards(ctx) {
		if f.AutoStart {
			n++
		}
	}
	return n
}

// RemovePortForward deletes a saved forward.
func (p *Prefs) RemovePortForward(f PortForward) {
	p.PortForwards = slices.DeleteFunc(p.PortForwards, func(x PortForward) bool { return samePF(x, f) })
}

// SavedForwards returns saved forwards of ctx.
func (p *Prefs) SavedForwards(ctx string) []PortForward {
	var out []PortForward
	for _, f := range p.PortForwards {
		if f.Context == ctx {
			out = append(out, f)
		}
	}
	return out
}

// AddBookmark adds or replaces a bookmark (same name in the same context).
func (p *Prefs) AddBookmark(b Bookmark) {
	p.RemoveBookmark(b)
	p.Bookmarks = append(p.Bookmarks, b)
}

// RemoveBookmark deletes the bookmark with b's name and context.
func (p *Prefs) RemoveBookmark(b Bookmark) {
	p.Bookmarks = slices.DeleteFunc(p.Bookmarks, func(x Bookmark) bool { return x.Name == b.Name && x.Context == b.Context })
}

// BookmarksFor returns bookmarks usable on ctx.
func (p *Prefs) BookmarksFor(ctx string) []Bookmark {
	var out []Bookmark
	for _, b := range p.Bookmarks {
		if b.Context == "" || b.Context == ctx {
			out = append(out, b)
		}
	}
	return out
}

// MigrateLegacyDirs moves config and cache directories created under the
// previous application name (k9desk) to the kubeon locations, once.
func MigrateLegacyDirs() {
	move := func(base string) {
		if base == "" {
			return
		}
		oldDir, newDir := filepath.Join(base, "k9desk"), filepath.Join(base, "kubeon")
		if _, err := os.Stat(newDir); err == nil {
			return
		}
		if st, err := os.Stat(oldDir); err == nil && st.IsDir() {
			_ = os.Rename(oldDir, newDir)
			// the log file keeps its old name inside; rename it too
			_ = os.Rename(filepath.Join(newDir, "k9desk.log"), filepath.Join(newDir, "kubeon.log"))
		}
	}
	if os.Getenv("KUBEON_CONFIG_DIR") == "" {
		if d, err := os.UserConfigDir(); err == nil {
			move(d)
		}
	}
	if d, err := os.UserCacheDir(); err == nil {
		move(d)
	}
}
