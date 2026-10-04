// Package prefs persists user preferences in ~/.config/k9desk/settings.json:
// theme, per-context safety policies, saved port-forwards and bookmarks.
package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
)

// ContextPolicy is the safety policy of a kube context.
type ContextPolicy struct {
	// Protected marks a production-like context: red frame, type-to-confirm
	// for destructive actions. nil = auto-detect from the context name.
	Protected *bool `json:"protected,omitempty"`
	// ReadOnly blocks every mutating action (UI and assistant proposals).
	ReadOnly bool `json:"readOnly,omitempty"`
}

// PortForward is a saved tunnel re-opened when the context connects.
type PortForward struct {
	Context   string `json:"context"`
	GVR       string `json:"gvr"`
	Path      string `json:"path"`
	Local     int    `json:"local"`
	Remote    int    `json:"remote"`
	AutoStart bool   `json:"autoStart"`
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

	mx   sync.Mutex
	path string
}

// Dir returns the config directory ($K9DESK_CONFIG_DIR overrides).
func Dir() string {
	if d := os.Getenv("K9DESK_CONFIG_DIR"); d != "" {
		return d
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "k9desk")
}

// Load reads the preferences (missing file → defaults).
func Load() *Prefs {
	p := Prefs{path: filepath.Join(Dir(), "settings.json")}
	if b, err := os.ReadFile(p.path); err == nil {
		_ = json.Unmarshal(b, &p)
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
	return os.WriteFile(p.path, b, 0o600)
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

// SetPolicy stores the policy of ctx.
func (p *Prefs) SetPolicy(ctx string, protected, readOnly bool) {
	c := p.Contexts[ctx]
	c.Protected, c.ReadOnly = &protected, readOnly
	p.Contexts[ctx] = c
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

// AddBookmark adds or replaces (by name) a bookmark.
func (p *Prefs) AddBookmark(b Bookmark) {
	p.Bookmarks = slices.DeleteFunc(p.Bookmarks, func(x Bookmark) bool { return x.Name == b.Name })
	p.Bookmarks = append(p.Bookmarks, b)
}

// RemoveBookmark deletes a bookmark by name.
func (p *Prefs) RemoveBookmark(name string) {
	p.Bookmarks = slices.DeleteFunc(p.Bookmarks, func(x Bookmark) bool { return x.Name == name })
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
