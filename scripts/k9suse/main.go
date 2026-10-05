// Command k9suse lists the vendored k9s API kubeon actually uses: every
// package-level object, method and field of third_party/k9s referenced by
// kubeon's own packages, one per line as "pkg<TAB>Name" or
// "pkg<TAB>Type.Member" (pkg relative to third_party/k9s, "." for its root).
// The upstream report matches apidiff's incompatible changes against it.
//
//	go run ./scripts/k9suse [packages]   (default ./internal/... ./cmd/...)
//
// Only the standard library: packages are type-checked from source against
// the compiler's export data (go list -export).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const k9s = "github.com/giovanni/kubeon/third_party/k9s"

type pkg struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	Export     string
	Imports    []string
	ImportMap  map[string]string
	DepOnly    bool
	Error      *struct{ Err string }
}

func main() {
	pats := os.Args[1:]
	if len(pats) == 0 {
		pats = []string{"./internal/...", "./cmd/..."}
	}
	args := append([]string{"list", "-e", "-export", "-deps", "-json", "-tags", "nox11,novulkan"}, pats...)
	cmd := exec.Command("go", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("go list: %v", err)
	}
	exports := map[string]string{}
	var roots []pkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p pkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
		if !p.DepOnly && !strings.HasPrefix(p.ImportPath, k9s) && importsK9s(p) {
			roots = append(roots, p)
		}
	}
	used := map[string]bool{}
	fset := token.NewFileSet()
	for _, p := range roots {
		imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
			if m, ok := p.ImportMap[path]; ok {
				path = m
			}
			f := exports[path]
			if f == "" {
				return nil, fmt.Errorf("no export data for %s", path)
			}
			return os.Open(f)
		})
		var files []*ast.File
		for _, f := range p.GoFiles {
			af, err := parser.ParseFile(fset, filepath.Join(p.Dir, f), nil, 0)
			if err != nil {
				log.Fatal(err)
			}
			files = append(files, af)
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
		conf := types.Config{Importer: imp, Error: func(error) {}}
		if _, err := conf.Check(p.ImportPath, fset, files, info); err != nil {
			log.Fatalf("type-check %s: %v", p.ImportPath, err)
		}
		// fields and methods, attributed to the type that declares them
		for _, sel := range info.Selections {
			obj := sel.Obj()
			if !fromK9s(obj) {
				continue
			}
			if decl := declaringType(sel); decl != nil {
				used[key(decl.Obj().Pkg(), decl.Obj().Name()+"."+obj.Name())] = true
			}
		}
		// package-level objects (functions, types, vars, consts)
		for _, obj := range info.Uses {
			if fromK9s(obj) && obj.Parent() == obj.Pkg().Scope() {
				used[key(obj.Pkg(), obj.Name())] = true
			}
		}
		// k9s types in the signatures of what kubeon calls: an interface we
		// implement and pass in (model.TableListener) must not change either
		for _, obj := range info.Uses {
			if fn, ok := obj.(*types.Func); ok && fromK9s(fn) {
				sig := fn.Type().(*types.Signature)
				for _, tup := range []*types.Tuple{sig.Params(), sig.Results()} {
					for i := 0; i < tup.Len(); i++ {
						markTypes(tup.At(i).Type(), used, 0)
					}
				}
			}
		}
	}
	lines := make([]string, 0, len(used))
	for k := range used {
		lines = append(lines, k)
	}
	sort.Strings(lines)
	fmt.Println(strings.Join(lines, "\n"))
}

// markTypes records the k9s named types reachable from t (shallow).
func markTypes(t types.Type, used map[string]bool, depth int) {
	if depth > 3 {
		return
	}
	switch x := t.(type) {
	case *types.Named:
		if fromK9s(x.Obj()) {
			used[key(x.Obj().Pkg(), x.Obj().Name())] = true
		}
	case *types.Pointer:
		markTypes(x.Elem(), used, depth+1)
	case *types.Slice:
		markTypes(x.Elem(), used, depth+1)
	case *types.Array:
		markTypes(x.Elem(), used, depth+1)
	case *types.Map:
		markTypes(x.Key(), used, depth+1)
		markTypes(x.Elem(), used, depth+1)
	case *types.Chan:
		markTypes(x.Elem(), used, depth+1)
	}
}

func importsK9s(p pkg) bool {
	for _, i := range p.Imports {
		if strings.HasPrefix(i, k9s) {
			return true
		}
	}
	return false
}

func fromK9s(obj types.Object) bool {
	return obj != nil && obj.Pkg() != nil && strings.HasPrefix(obj.Pkg().Path(), k9s)
}

func key(p *types.Package, name string) string {
	rel := strings.TrimPrefix(strings.TrimPrefix(p.Path(), k9s), "/")
	if rel == "" {
		rel = "."
	}
	return rel + "\t" + name
}

// declaringType follows embedded fields to the named type that declares the
// selected field or method.
func declaringType(sel *types.Selection) *types.Named {
	if fn, ok := sel.Obj().(*types.Func); ok {
		if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
			return named(recv.Type())
		}
	}
	t := sel.Recv()
	idx := sel.Index()
	for _, i := range idx[:len(idx)-1] {
		st, ok := deref(t).Underlying().(*types.Struct)
		if !ok {
			return nil
		}
		t = st.Field(i).Type()
	}
	return named(t)
}

func deref(t types.Type) types.Type {
	if p, ok := t.(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

func named(t types.Type) *types.Named {
	n, _ := deref(t).(*types.Named)
	return n
}
