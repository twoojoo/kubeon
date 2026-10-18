// Package site generates the static website published on GitHub Pages
// (landing page, one page per feature, Kubeon vs Lens, sitemap.xml,
// llms.txt). Build it with `make site`.
package site

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	ttemplate "text/template"
)

const (
	DefaultURL  = "https://twoojoo.github.io/kubeon/"
	RepoURL     = "https://github.com/twoojoo/kubeon"
	DownloadURL = "https://github.com/twoojoo/kubeon/releases/latest"
	licenseURL  = "https://www.gnu.org/licenses/gpl-3.0.html"
)

//go:embed templates static
var files embed.FS

type Options struct {
	BaseURL string // public URL with the trailing slash (DefaultURL if empty)
}

// Page is one generated HTML page.
type Page struct {
	Path        string // relative to the base URL: "", "features/", "features/logs/"
	Template    string
	Title       string
	Description string
	Crumbs      []Crumb // breadcrumbs after Home
	Feature     *FeaturePage
	jsonLD      []any
}

type Crumb struct{ Name, Path string }

// FeaturePage is a feature page with its neighbours.
type FeaturePage struct {
	Feature
	Related []*FeaturePage
}

type site struct {
	base     string
	pages    []*Page
	features []*FeaturePage
}

// data is what templates see.
type data struct {
	*Page
	Base      string
	Name      string
	Tagline   string
	Summary   string
	Repo      string
	Download  string
	Cards     []card
	FAQ       []faq
	Shots     []shot
	Features  []*FeaturePage
	Compare   []compareRow
	LensQA    []faq
	JSONLD    template.HTML
}

// Build writes the site into dir, replacing its content.
func Build(dir string, o Options) error {
	s, err := newSite(o)
	if err != nil {
		return err
	}
	out, err := s.render()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	for name, b := range out {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func newSite(o Options) (*site, error) {
	base := o.BaseURL
	if base == "" {
		base = DefaultURL
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	s := &site{base: base}
	for _, f := range features {
		s.features = append(s.features, &FeaturePage{Feature: f})
	}
	// related pages: the next three in the list
	for i, p := range s.features {
		for k := 1; k <= 3 && k < len(s.features); k++ {
			p.Related = append(p.Related, s.features[(i+k)%len(s.features)])
		}
	}

	home := &Page{Template: "index.html", Title: "Kubeon — native open-source Kubernetes IDE, Lens alternative",
		Description: "Free, native Kubernetes IDE built on the k9s engine: live tables, logs, shell, port-forwards, Helm, XRay. No Electron. Linux, Windows, macOS."}
	home.jsonLD = []any{s.app(home, "", nil), s.website(), faqLD(homeFAQ)}
	list := &Page{Path: "features/", Template: "features.html", Title: "Features — Kubeon Kubernetes IDE",
		Description: "Everything Kubeon does: log viewer, pod shell, port-forward manager, Helm, XRay, YAML editing with diff, RBAC explorer, sanitizer, AI assistant.",
		Crumbs:      []Crumb{{"Features", "features/"}}}
	list.jsonLD = []any{s.breadcrumbs(list)}
	vs := &Page{Path: "kubeon-vs-lens/", Template: "lens.html", Title: "Kubeon vs Lens: a native, open-source Lens alternative",
		Description: "Kubeon vs Lens Desktop: runtime, license, account, extensions, production safety, AI. What each does better, and when to pick which.",
		Crumbs:      []Crumb{{"Kubeon vs Lens", "kubeon-vs-lens/"}}}
	vs.jsonLD = []any{s.breadcrumbs(vs), faqLD(lensFAQ)}
	s.pages = []*Page{home, list, vs}
	for _, f := range s.features {
		p := &Page{Path: "features/" + f.Slug + "/", Template: "feature.html", Title: f.Title, Description: f.Description, Feature: f,
			Crumbs: []Crumb{{"Features", "features/"}, {f.Name, "features/" + f.Slug + "/"}}}
		p.jsonLD = []any{s.app(p, f.Name, f.Highlights), s.breadcrumbs(p)}
		s.pages = append(s.pages, p)
	}
	return s, nil
}

func (s *site) abs(p string) string { return s.base + p }

func (s *site) app(p *Page, suffix string, featureList []string) map[string]any {
	n := name
	if suffix != "" {
		n = name + " — " + suffix
	}
	shotURLs := []string{}
	for _, sh := range shots {
		shotURLs = append(shotURLs, s.abs("img/"+sh.File))
	}
	m := map[string]any{
		"@context":               "https://schema.org",
		"@type":                  "SoftwareApplication",
		"name":                   n,
		"url":                    s.abs(p.Path),
		"description":            p.Description,
		"applicationCategory":    "DeveloperApplication",
		"applicationSubCategory": "Kubernetes IDE",
		"operatingSystem":        "Linux, Windows 10+, macOS 12+",
		"offers":                 map[string]any{"@type": "Offer", "price": "0", "priceCurrency": "USD"},
		"license":                licenseURL,
		"downloadUrl":            DownloadURL,
		"installUrl":             DownloadURL,
		"image":                  s.abs("img/logo.png"),
		"screenshot":             shotURLs,
		"isAccessibleForFree":    true,
		"sameAs":                 []string{RepoURL},
		"author":                 map[string]any{"@type": "Person", "name": "twoojoo", "url": "https://github.com/twoojoo"},
	}
	if len(featureList) > 0 {
		m["featureList"] = featureList
	} else {
		var fl []string
		for _, c := range cards {
			fl = append(fl, c.Title)
		}
		m["featureList"] = fl
	}
	return m
}

func (s *site) website() map[string]any {
	return map[string]any{"@context": "https://schema.org", "@type": "WebSite", "name": name, "url": s.base}
}

func (s *site) breadcrumbs(p *Page) map[string]any {
	items := []any{map[string]any{"@type": "ListItem", "position": 1, "name": "Home", "item": s.base}}
	for i, c := range p.Crumbs {
		items = append(items, map[string]any{"@type": "ListItem", "position": i + 2, "name": c.Name, "item": s.abs(c.Path)})
	}
	return map[string]any{"@context": "https://schema.org", "@type": "BreadcrumbList", "itemListElement": items}
}

func faqLD(qq []faq) map[string]any {
	var items []any
	for _, q := range qq {
		items = append(items, map[string]any{"@type": "Question", "name": q.Q,
			"acceptedAnswer": map[string]any{"@type": "Answer", "text": q.A}})
	}
	return map[string]any{"@context": "https://schema.org", "@type": "FAQPage", "mainEntity": items}
}

// render returns every output file by slash path.
func (s *site) render() (map[string][]byte, error) {
	out := map[string][]byte{}
	// rel/abs are replaced per page (template functions are resolved at execution)
	layout, err := template.New("").Funcs(template.FuncMap{"rel": s.abs, "abs": s.abs}).ParseFS(files, "templates/layout.html")
	if err != nil {
		return nil, err
	}
	for _, p := range s.pages {
		depth := strings.Count(p.Path, "/")
		rel := func(target string) string {
			r := strings.Repeat("../", depth) + target
			if r == "" {
				return "./"
			}
			return r
		}
		t, err := layout.Clone()
		if err != nil {
			return nil, err
		}
		t = t.Funcs(template.FuncMap{"rel": rel, "abs": s.abs})
		if _, err := t.ParseFS(files, "templates/"+p.Template); err != nil {
			return nil, err
		}
		var ld bytes.Buffer
		for _, x := range p.jsonLD {
			j, err := json.MarshalIndent(x, "", "  ") // escapes <, > and &
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&ld, "<script type=\"application/ld+json\">\n%s\n</script>\n", j)
		}
		var b bytes.Buffer
		if err := t.ExecuteTemplate(&b, "layout", s.data(p, template.HTML(ld.String()))); err != nil {
			return nil, fmt.Errorf("%s: %w", p.Template, err)
		}
		out[p.Path+"index.html"] = b.Bytes()
	}

	// 404: GitHub Pages serves it for any missing path, so links are absolute
	nf, err := layout.Clone()
	if err != nil {
		return nil, err
	}
	nf = nf.Funcs(template.FuncMap{"rel": s.abs, "abs": s.abs})
	if _, err := nf.ParseFS(files, "templates/404.html"); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	notFound := &Page{Path: "404.html", Template: "404.html", Title: "Page not found — Kubeon", Description: "This page doesn't exist."}
	if err := nf.ExecuteTemplate(&b, "layout", s.data(notFound, "")); err != nil {
		return nil, err
	}
	out["404.html"] = b.Bytes()

	for name, tmpl := range map[string]string{"llms.txt": "templates/llms.txt", "llms-full.txt": "templates/llms-full.txt", "sitemap.xml": "templates/sitemap.xml"} {
		t, err := ttemplate.New(path.Base(tmpl)).Funcs(ttemplate.FuncMap{"abs": s.abs}).ParseFS(files, tmpl)
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		if err := t.Execute(&b, struct {
			data
			Pages []*Page
		}{s.data(&Page{}, ""), s.pages}); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = b.Bytes()
	}

	err = fs.WalkDir(files, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := files.ReadFile(p)
		out[strings.TrimPrefix(p, "static/")] = b
		return err
	})
	// a .nojekyll file: GitHub Pages serves the files as they are
	out[".nojekyll"] = nil
	return out, err
}

func (s *site) data(p *Page, ld template.HTML) data {
	return data{Page: p, Base: s.base, Name: name, Tagline: tagline, Summary: summary, Repo: RepoURL, Download: DownloadURL,
		Cards: cards, FAQ: homeFAQ, Shots: shots, Features: s.features, Compare: lensRows, LensQA: lensFAQ, JSONLD: ld}
}
